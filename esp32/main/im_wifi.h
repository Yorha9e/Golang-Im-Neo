/* im_wifi.h — STA connect with backoff reconnect (ESP-IDF side). */
#ifndef IM_WIFI_H
#define IM_WIFI_H

#include <stdbool.h>
#include <stdint.h>

void im_wifi_start(void);
bool im_wifi_is_connected(void);
bool im_wifi_wait_connected(uint32_t timeout_ms);

#endif /* IM_WIFI_H */
