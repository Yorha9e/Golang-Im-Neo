/* im_wifi.c — WiFi STA bring-up with exponential-backoff reconnect.
 *
 * One static event group tracks GOT_IP. On STA_DISCONNECTED a one-shot
 * esp_timer re-arms esp_wifi_connect() after min(2^n s, 60 s); a GOT_IP
 * event resets the counter. All state is static; nothing is allocated
 * after im_wifi_start() returns.
 */
#include "im_wifi.h"

#include <string.h>

#include "esp_event.h"
#include "esp_log.h"
#include "esp_netif.h"
#include "esp_timer.h"
#include "esp_wifi.h"
#include "freertos/FreeRTOS.h"
#include "freertos/event_groups.h"
#include "freertos/task.h"
#include "sdkconfig.h"

static const char *TAG = "im_wifi";

#define IM_WIFI_CONNECTED_BIT BIT0
#define IM_WIFI_BACKOFF_MAX_S 60u

static EventGroupHandle_t s_wifi_events;
static esp_timer_handle_t s_backoff;
static uint32_t s_retries;

static void im_wifi_backoff_cb(void *arg)
{
    (void)arg;
    if (esp_wifi_connect() != ESP_OK) {
        ESP_LOGW(TAG, "reconnect call failed, will retry on next event");
    }
}

static void im_wifi_arm_backoff(void)
{
    uint32_t shift;
    uint64_t delay_s;
    if (s_backoff == NULL) {
        return;
    }
    shift = (s_retries < 6u) ? s_retries : 6u; /* cap 2^6 = 64 -> 60 s max */
    delay_s = (uint64_t)1u << shift;
    if (delay_s > IM_WIFI_BACKOFF_MAX_S) {
        delay_s = IM_WIFI_BACKOFF_MAX_S;
    }
    s_retries++;
    ESP_LOGI(TAG, "reconnect in %u s (attempt %u)", (unsigned)delay_s,
             (unsigned)s_retries);
    esp_timer_stop(s_backoff);
    (void)esp_timer_start_once(s_backoff, delay_s * 1000000ULL);
}

static void im_wifi_event_handler(void *arg, esp_event_base_t base,
                                  int32_t id, void *data)
{
    (void)arg;
    (void)data;
    if (base == WIFI_EVENT && id == WIFI_EVENT_STA_DISCONNECTED) {
        xEventGroupClearBits(s_wifi_events, IM_WIFI_CONNECTED_BIT);
        ESP_LOGW(TAG, "disconnected, arming backoff");
        im_wifi_arm_backoff();
    } else if (base == IP_EVENT && id == IP_EVENT_STA_GOT_IP) {
        s_retries = 0;
        xEventGroupSetBits(s_wifi_events, IM_WIFI_CONNECTED_BIT);
        ESP_LOGI(TAG, "got IP");
    }
}

void im_wifi_start(void)
{
    static bool started;
    wifi_init_config_t init_cfg = WIFI_INIT_CONFIG_DEFAULT();
    wifi_config_t sta_cfg;
    esp_event_handler_instance_t h1;
    esp_event_handler_instance_t h2;
    const esp_timer_create_args_t targs = {
        .callback = im_wifi_backoff_cb,
        .name = "wifi_backoff",
    };
    if (started) {
        return;
    }
    started = true;

    s_wifi_events = xEventGroupCreate();
    (void)esp_timer_create(&targs, &s_backoff);

    ESP_ERROR_CHECK(esp_netif_init());
    ESP_ERROR_CHECK(esp_event_loop_create_default());
    (void)esp_netif_create_default_wifi_sta();
    ESP_ERROR_CHECK(esp_wifi_init(&init_cfg));
    ESP_ERROR_CHECK(esp_event_handler_instance_register(
        WIFI_EVENT, ESP_EVENT_ANY_ID, im_wifi_event_handler, NULL, &h1));
    ESP_ERROR_CHECK(esp_event_handler_instance_register(
        IP_EVENT, IP_EVENT_STA_GOT_IP, im_wifi_event_handler, NULL, &h2));
    (void)h1;
    (void)h2;

    memset(&sta_cfg, 0, sizeof(sta_cfg));
    snprintf((char *)sta_cfg.sta.ssid, sizeof(sta_cfg.sta.ssid), "%s",
             CONFIG_ESP_IM_WIFI_SSID);
    snprintf((char *)sta_cfg.sta.password, sizeof(sta_cfg.sta.password), "%s",
             CONFIG_ESP_IM_WIFI_PASSWORD);
    sta_cfg.sta.threshold.authmode = WIFI_AUTH_WPA2_PSK;
    sta_cfg.sta.pmf_cfg.capable = true;
    sta_cfg.sta.pmf_cfg.required = false;

    ESP_ERROR_CHECK(esp_wifi_set_mode(WIFI_MODE_STA));
    ESP_ERROR_CHECK(esp_wifi_set_config(WIFI_IF_STA, &sta_cfg));
    ESP_ERROR_CHECK(esp_wifi_start());
    ESP_LOGI(TAG, "connecting to SSID %s", CONFIG_ESP_IM_WIFI_SSID);
    ESP_ERROR_CHECK(esp_wifi_connect());
}

bool im_wifi_is_connected(void)
{
    if (s_wifi_events == NULL) {
        return false;
    }
    return (xEventGroupGetBits(s_wifi_events) & IM_WIFI_CONNECTED_BIT) != 0;
}

bool im_wifi_wait_connected(uint32_t timeout_ms)
{
    EventBits_t bits;
    TickType_t ticks = (timeout_ms == 0) ? portMAX_DELAY
                                         : pdMS_TO_TICKS(timeout_ms);
    if (s_wifi_events == NULL) {
        return false;
    }
    bits = xEventGroupWaitBits(s_wifi_events, IM_WIFI_CONNECTED_BIT, pdFALSE,
                               pdTRUE, ticks);
    return (bits & IM_WIFI_CONNECTED_BIT) != 0;
}
