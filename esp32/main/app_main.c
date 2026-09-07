/* app_main.c — ESP32-C3 IM client orchestrator.
 *
 * Boot: NVS -> WiFi STA -> link task. The link task runs the session loop:
 *   wait WiFi -> POST /api/v1/auth/login (device_class "hardware") ->
 *   POST /api/v1/auth/ticket (Bearer) -> ws://host:port/ws?ticket=... ->
 *   supervise (1 s tick: hello CHAT once, 30 s heartbeat, 90 s pong
 *   timeout) -> teardown -> exponential backoff -> full re-login.
 *
 * Re-login on every reconnect (instead of caching tokens) keeps the logic
 * simple and matches short server token TTLs; JWT + ticket live in static
 * buffers sized for ~2 KB credentials.
 */
#include <string.h>

#include "esp_http_client.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "nvs_flash.h"
#include "sdkconfig.h"

#include "im_proto.h"
#include "im_wifi.h"
#include "im_ws_client.h"

static const char *TAG = "im_main";

#define IM_HTTP_RESP 4096u
#define IM_TOKEN_BUF 2048u
#define IM_BODY_BUF 384u
#define IM_URL_BUF 256u
#define IM_PONG_TIMEOUT_MS 90000u
#define IM_BACKOFF_MAX_S 60u

static char s_resp[IM_HTTP_RESP];
static char s_access_token[IM_TOKEN_BUF];
static char s_ticket[IM_TOKEN_BUF];
static char s_body[IM_BODY_BUF];
static char s_url[IM_URL_BUF];

static uint32_t im_now_ms(void)
{
    return (uint32_t)(esp_timer_get_time() / 1000LL);
}

/* Minimal JSON string extractor: finds "key" : "value" and copies value.
 * Handles \" escapes; anything fancier (nested objects, \uXXXX) is out of
 * scope — the auth envelope is flat ({code,msg,data:{...}}). */
static bool im_json_get(const char *json, const char *key, char *out,
                        size_t out_sz)
{
    char pat[64];
    const char *p;
    size_t o = 0u;
    if (json == NULL || key == NULL || out == NULL || out_sz == 0u) {
        return false;
    }
    snprintf(pat, sizeof(pat), "\"%s\"", key);
    p = strstr(json, pat);
    if (p == NULL) {
        return false;
    }
    p = strchr(p + strlen(pat), ':');
    if (p == NULL) {
        return false;
    }
    p = strchr(p + 1u, '"');
    if (p == NULL) {
        return false;
    }
    p++;
    while (*p != '\0' && *p != '"') {
        if (*p == '\\' && *(p + 1u) != '\0') {
            p++;
        }
        if (o + 1u >= out_sz) {
            return false;
        }
        out[o++] = *p++;
    }
    if (*p != '"') {
        return false;
    }
    out[o] = '\0';
    return o > 0u;
}

static bool im_http_post(const char *url, const char *body,
                         const char *bearer, int *status)
{
    esp_http_client_config_t cfg;
    esp_http_client_handle_t cli;
    char auth[IM_TOKEN_BUF + 8u];
    int len;
    memset(&cfg, 0, sizeof(cfg));
    cfg.url = url;
    cfg.timeout_ms = 10000;
    cli = esp_http_client_init(&cfg);
    if (cli == NULL) {
        return false;
    }
    (void)esp_http_client_set_method(cli, HTTP_METHOD_POST);
    (void)esp_http_client_set_header(cli, "Content-Type", "application/json");
    if (bearer != NULL) {
        snprintf(auth, sizeof(auth), "Bearer %s", bearer);
        (void)esp_http_client_set_header(cli, "Authorization", auth);
    }
    if (body != NULL) {
        (void)esp_http_client_set_post_field(cli, body, (int)strlen(body));
    }
    memset(s_resp, 0, sizeof(s_resp));
    if (esp_http_client_perform(cli) != ESP_OK) {
        ESP_LOGW(TAG, "POST %s failed", url);
        (void)esp_http_client_cleanup(cli);
        return false;
    }
    if (status != NULL) {
        *status = esp_http_client_get_status_code(cli);
    }
    len = esp_http_client_read_response(cli, s_resp, (int)sizeof(s_resp) - 1);
    (void)esp_http_client_cleanup(cli);
    if (len < 0) {
        return false;
    }
    s_resp[(size_t)len] = '\0';
    return true;
}

static bool im_do_login(void)
{
    int status = 0;
    snprintf(s_url, sizeof(s_url), "http://%s:%d/api/v1/auth/login",
             CONFIG_ESP_IM_SERVER_HOST, CONFIG_ESP_IM_SERVER_PORT);
    snprintf(s_body, sizeof(s_body),
             "{\"username\":\"%s\",\"password\":\"%s\","
             "\"device_class\":\"hardware\",\"device_name\":\"esp32c3-01\"}",
             CONFIG_ESP_IM_AUTH_USERNAME, CONFIG_ESP_IM_AUTH_PASSWORD);
    if (!im_http_post(s_url, s_body, NULL, &status) || status != 200) {
        ESP_LOGW(TAG, "login http status %d", status);
        return false;
    }
    memset(s_access_token, 0, sizeof(s_access_token));
    if (!im_json_get(s_resp, "access_token", s_access_token,
                     sizeof(s_access_token))) {
        ESP_LOGW(TAG, "login: no access_token in envelope");
        return false;
    }
    ESP_LOGI(TAG, "login ok");
    return true;
}

static bool im_do_ticket(void)
{
    int status = 0;
    snprintf(s_url, sizeof(s_url), "http://%s:%d/api/v1/auth/ticket",
             CONFIG_ESP_IM_SERVER_HOST, CONFIG_ESP_IM_SERVER_PORT);
    if (!im_http_post(s_url, "{}", s_access_token, &status) ||
        status != 200) {
        ESP_LOGW(TAG, "ticket http status %d", status);
        return false;
    }
    memset(s_ticket, 0, sizeof(s_ticket));
    if (!im_json_get(s_resp, "ticket", s_ticket, sizeof(s_ticket))) {
        ESP_LOGW(TAG, "ticket: no ticket in envelope");
        return false;
    }
    ESP_LOGI(TAG, "ticket ok");
    return true;
}

static void im_backoff_sleep(uint32_t *backoff_s)
{
    uint32_t d = (*backoff_s > IM_BACKOFF_MAX_S) ? IM_BACKOFF_MAX_S
                                                 : *backoff_s;
    ESP_LOGI(TAG, "retry in %u s", (unsigned)d);
    vTaskDelay(pdMS_TO_TICKS(d * 1000u));
    *backoff_s = d * 2u;
    if (*backoff_s > IM_BACKOFF_MAX_S) {
        *backoff_s = IM_BACKOFF_MAX_S;
    }
    if (*backoff_s == 0u) {
        *backoff_s = 1u;
    }
}

static void im_link_task(void *arg)
{
    static uint32_t hello_n;
    uint32_t backoff_s = 1u;
    (void)arg;
    for (;;) {
        bool hello_sent = false;
        char stanza[48];
        im_wifi_wait_connected(0u);
        ESP_LOGI(TAG, "wifi up, logging in");
        if (!im_do_login()) {
            im_backoff_sleep(&backoff_s);
            continue;
        }
        if (!im_do_ticket()) {
            im_backoff_sleep(&backoff_s);
            continue;
        }
        if (!im_ws_start(CONFIG_ESP_IM_SERVER_HOST,
                         (uint16_t)CONFIG_ESP_IM_SERVER_PORT, s_ticket)) {
            ESP_LOGW(TAG, "ws start failed");
            im_backoff_sleep(&backoff_s);
            continue;
        }
        memset(s_ticket, 0, sizeof(s_ticket)); /* single-use: drop ASAP */
        backoff_s = 1u;
        ESP_LOGI(TAG, "session live");
        for (;;) {
            uint32_t now;
            vTaskDelay(pdMS_TO_TICKS(1000));
            now = im_now_ms();
            if (!im_wifi_is_connected() || !im_ws_is_connected()) {
                ESP_LOGW(TAG, "link lost, re-establishing");
                break;
            }
            if (!hello_sent) {
                hello_sent = true;
                hello_n++;
                snprintf(stanza, sizeof(stanza), "esp32c3-hello-%u",
                         (unsigned)hello_n);
                (void)im_ws_send_chat("", "hello from esp32c3-01", stanza);
            }
            if (!im_ws_poll_heartbeat(now)) {
                ESP_LOGW(TAG, "heartbeat failed, reconnecting");
                break;
            }
            if ((now - im_ws_last_pong_ms()) > IM_PONG_TIMEOUT_MS) {
                ESP_LOGW(TAG, "pong timeout (90 s), reconnecting");
                break;
            }
        }
        im_ws_stop();
        im_backoff_sleep(&backoff_s);
    }
}

void app_main(void)
{
    esp_err_t r = nvs_flash_init();
    if (r == ESP_ERR_NVS_NO_FREE_PAGES ||
        r == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        (void)nvs_flash_erase();
        r = nvs_flash_init();
    }
    ESP_ERROR_CHECK(r);
    im_wifi_start();
    if (xTaskCreate(im_link_task, "im_link", 6144, NULL, 5, NULL) != pdPASS) {
        ESP_LOGE(TAG, "link task create failed");
    }
}
