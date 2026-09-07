/* im_ws_client.c — binary WebSocket data path (see im_ws_client.h).
 *
 * #define IM_ZERO_MALLOC 1
 * ZERO-HEAP AUDIT (data path = this file + im_proto.c):
 *  - RX: static s_rx[1400] reassembly buffer; frames larger than that are
 *    dropped before any copy. Multi-part deliveries are appended by
 *    payload_offset and decoded only when the final part arrives.
 *  - Decode: static proto_WsMessage s_msg (stack-sized struct, ~1 KB);
 *    im_proto_decode() zeroes it first, so malformed input fails closed.
 *  - TX: static s_tx[256]; every send goes through im_proto_build_ping /
 *    im_proto_build_chat, which guarantee the encoded frame fits 256 B.
 *    im_send_raw() re-checks len <= 256 and drops anything larger.
 *  - URL: static s_url[]; ticket is percent-encoded inline, bounded.
 *  - Setup-only handles (esp_websocket_client handle, event registration)
 *    are created once in im_ws_start() and destroyed in im_ws_stop(); the
 *    steady-state RX/TX/heartbeat path performs no heap activity at all.
 *  - The echo reply reuses s_tx; no scratch is borrowed from anywhere else.
 */
#define IM_ZERO_MALLOC 1

#include "im_ws_client.h"

#include <string.h>

#include "esp_log.h"
#include "esp_timer.h"
#include "esp_websocket_client.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#include "im_proto.h"

static const char *TAG = "im_ws";

/* RX budget: largest plausible inbound envelope (proto_WsMessage_size is
 * 1066; the server truncates hardware-bound *content* to 256 B but the
 * frame keeps its envelope, so RX stays roomy while TX is capped at 256). */
#define IM_RX_BUF 1400u
#define IM_URL_BUF 2048u

static esp_websocket_client_handle_t s_client;
static bool s_connected;
static uint32_t s_last_ping_ms;
static uint32_t s_last_pong_ms;
static uint8_t s_rx[IM_RX_BUF];
static size_t s_rx_off;
static uint8_t s_tx[IM_MAX_FRAME];
static proto_WsMessage s_msg;
static char s_url[IM_URL_BUF];

static uint32_t im_now_ms(void)
{
    return (uint32_t)(esp_timer_get_time() / 1000LL);
}

/* Percent-encode src into the URL tail; returns false when dst would
 * overflow (caller drops the connect attempt instead of truncating). */
static bool im_url_append(char *dst, size_t dst_sz, size_t *pos,
                          const char *src)
{
    static const char kHex[] = "0123456789ABCDEF";
    size_t i = 0u;
    if (dst == NULL || pos == NULL || src == NULL) {
        return false;
    }
    while (src[i] != '\0') {
        unsigned char c = (unsigned char)src[i];
        bool plain = (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
                     (c >= '0' && c <= '9') || c == '-' || c == '_' ||
                     c == '.' || c == '~';
        if (plain) {
            if (*pos + 1u >= dst_sz) {
                return false;
            }
            dst[*pos] = (char)c;
            (*pos)++;
        } else {
            if (*pos + 3u >= dst_sz) {
                return false;
            }
            dst[*pos] = '%';
            dst[*pos + 1u] = kHex[(c >> 4) & 0xFu];
            dst[*pos + 2u] = kHex[c & 0xFu];
            *pos += 3u;
        }
        i++;
    }
    dst[*pos] = '\0';
    return true;
}

/* Raw binary send; the 256 B ceiling is enforced here as well as in the
 * im_proto builders (defence in depth: builders guarantee, transport
 * verifies). */
static bool im_send_raw(const uint8_t *data, size_t len)
{
    int rc;
    if (s_client == NULL || !s_connected || data == NULL || len == 0u ||
        len > IM_MAX_FRAME) {
        return false;
    }
    rc = esp_websocket_client_send_bin(s_client, (const char *)data, (int)len,
                                       pdMS_TO_TICKS(5000));
    return rc >= 0;
}

static bool im_send_ping(void)
{
    size_t n = 0u;
    if (!im_proto_build_ping(s_tx, sizeof(s_tx), &n)) {
        return false;
    }
    return im_send_raw(s_tx, n);
}

static void im_handle_frame(const uint8_t *data, size_t len)
{
    proto_MsgType t;
    if (!im_proto_decode(data, len, &s_msg)) {
        ESP_LOGW(TAG, "drop malformed frame (%u B)", (unsigned)len);
        return;
    }
    t = im_proto_classify(&s_msg);
    switch (t) {
    case proto_MsgType_HEARTBEAT_PONG:
        s_last_pong_ms = im_now_ms();
        break;
    case proto_MsgType_PRIVATE_CHAT:
        ESP_LOGI(TAG, "priv from=%.35s stanza=%.63s: %.128s", s_msg.from_uid,
                 s_msg.stanza_id, s_msg.content);
        /* Canned demo reply to the sender, correlated by stanza. Short by
         * construction, but still routed via the capped builder. */
        if (!im_ws_send_chat(s_msg.from_uid, "esp32c3-01: got it",
                             s_msg.stanza_id)) {
            ESP_LOGW(TAG, "echo reply dropped");
        }
        break;
    case proto_MsgType_CHAT:
        ESP_LOGI(TAG, "chat from=%.35s: %.128s", s_msg.from_uid,
                 s_msg.content);
        break;
    case proto_MsgType_ACK:
        ESP_LOGI(TAG, "ack stanza=%.63s seq=%lld", s_msg.stanza_id,
                 (long long)s_msg.seq);
        break;
    case proto_MsgType_SYSTEM_NOTICE:
        ESP_LOGI(TAG, "notice: %.128s", s_msg.content);
        break;
    default:
        ESP_LOGD(TAG, "drop unsupported type %d", (int)t);
        break;
    }
}

static void im_ws_event_handler(void *arg, esp_event_base_t base, int32_t id,
                                void *event_data)
{
    esp_websocket_event_data_t *ev = (esp_websocket_event_data_t *)event_data;
    (void)arg;
    (void)base;
    if (ev == NULL) {
        return;
    }
    switch (id) {
    case WEBSOCKET_EVENT_CONNECTED:
        ESP_LOGI(TAG, "ws connected");
        s_connected = true;
        s_last_ping_ms = 0u; /* force an immediate first heartbeat */
        s_last_pong_ms = im_now_ms();
        s_rx_off = 0u;
        break;
    case WEBSOCKET_EVENT_DISCONNECTED:
    case WEBSOCKET_EVENT_CLOSED:
        ESP_LOGW(TAG, "ws closed");
        s_connected = false;
        s_rx_off = 0u;
        break;
    case WEBSOCKET_EVENT_DATA:
        if (ev->op_code != 2) {
            break; /* binary protobuf frames only */
        }
        if (ev->payload_len <= 0 ||
            (size_t)ev->payload_len > sizeof(s_rx)) {
            ESP_LOGW(TAG, "drop oversize frame (%d B)", ev->payload_len);
            s_rx_off = 0u;
            break;
        }
        if (ev->payload_offset == 0) {
            s_rx_off = 0u;
        }
        if (ev->payload_offset != (int)s_rx_off) {
            ESP_LOGW(TAG, "rx gap, restarting assembly");
            s_rx_off = 0u;
            break;
        }
        if (s_rx_off + (size_t)ev->data_len > sizeof(s_rx)) {
            ESP_LOGW(TAG, "rx overflow, dropping");
            s_rx_off = 0u;
            break;
        }
        memcpy(&s_rx[s_rx_off], ev->data_ptr, (size_t)ev->data_len);
        s_rx_off += (size_t)ev->data_len;
        if (s_rx_off < (size_t)ev->payload_len) {
            break; /* wait for remaining parts */
        }
        im_handle_frame(s_rx, s_rx_off);
        s_rx_off = 0u;
        break;
    case WEBSOCKET_EVENT_ERROR:
        ESP_LOGW(TAG, "ws transport error");
        break;
    default:
        break;
    }
}

bool im_ws_start(const char *host, uint16_t port, const char *ticket)
{
    esp_websocket_client_config_t cfg;
    size_t pos = 0u;
    int n;
    if (host == NULL || ticket == NULL || s_client != NULL) {
        return false;
    }
    memset(s_url, 0, sizeof(s_url));
    n = snprintf(s_url, sizeof(s_url), "ws://%s:%u/ws?ticket=", host,
                 (unsigned)port);
    if (n <= 0 || (size_t)n >= sizeof(s_url)) {
        return false;
    }
    pos = (size_t)n;
    if (!im_url_append(s_url, sizeof(s_url), &pos, ticket)) {
        ESP_LOGE(TAG, "ticket too long for URL buffer");
        return false;
    }

    memset(&cfg, 0, sizeof(cfg));
    cfg.uri = s_url;
    cfg.buffer_size = IM_RX_BUF + 256u;
    cfg.task_stack = 6144;
    cfg.network_timeout_ms = 10000;

    s_connected = false;
    s_rx_off = 0u;
    s_client = esp_websocket_client_init(&cfg);
    if (s_client == NULL) {
        ESP_LOGE(TAG, "ws init failed");
        return false;
    }
    if (esp_websocket_client_register_events(
            s_client, WEBSOCKET_EVENT_ANY, im_ws_event_handler, NULL) !=
        ESP_OK) {
        ESP_LOGE(TAG, "ws event registration failed");
        (void)esp_websocket_client_destroy(s_client);
        s_client = NULL;
        return false;
    }
    if (esp_websocket_client_start(s_client) != ESP_OK) {
        ESP_LOGE(TAG, "ws start failed");
        (void)esp_websocket_client_destroy(s_client);
        s_client = NULL;
        return false;
    }
    return true;
}

void im_ws_stop(void)
{
    if (s_client != NULL) {
        (void)esp_websocket_client_stop(s_client);
        (void)esp_websocket_client_destroy(s_client);
        s_client = NULL;
    }
    s_connected = false;
    s_rx_off = 0u;
}

bool im_ws_is_connected(void)
{
    return s_client != NULL && s_connected &&
           esp_websocket_client_is_connected(s_client);
}

uint32_t im_ws_last_pong_ms(void)
{
    return s_last_pong_ms;
}

bool im_ws_poll_heartbeat(uint32_t now_ms)
{
    if (!im_ws_is_connected()) {
        return true;
    }
    if ((now_ms - s_last_ping_ms) < IM_HEARTBEAT_PERIOD_MS) {
        return true;
    }
    s_last_ping_ms = now_ms;
    if (!im_send_ping()) {
        ESP_LOGW(TAG, "heartbeat send failed");
        return false;
    }
    ESP_LOGD(TAG, "ping sent");
    return true;
}

bool im_ws_send_chat(const char *to_uid, const char *content,
                     const char *stanza_id)
{
    im_proto_status_t st;
    size_t n = 0u;
    proto_MsgType type =
        (to_uid != NULL && to_uid[0] != '\0') ? proto_MsgType_PRIVATE_CHAT
                                              : proto_MsgType_CHAT;
    st = im_proto_build_chat(type, to_uid, content, stanza_id, s_tx,
                             sizeof(s_tx), &n);
    if (st == IM_PROTO_ERR) {
        ESP_LOGW(TAG, "chat build failed");
        return false;
    }
    if (st == IM_PROTO_TRUNCATED) {
        ESP_LOGW(TAG, "chat content shortened to fit 256 B frame");
    }
    return im_send_raw(s_tx, n);
}
