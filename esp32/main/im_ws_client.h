/* im_ws_client.h — binary WebSocket transport over im_proto (ESP-IDF side).
 *
 * The link task in app_main.c owns policy (login, backoff, pong timeout);
 * this unit owns mechanism: one client handle, static RX/TX buffers, RX
 * reassembly, im_proto decode/classify, 30 s heartbeat send, and a minimal
 * demo path (one hello CHAT per connection is sent by the link task; an
 * inbound PRIVATE_CHAT gets a short canned reply here).
 */
#ifndef IM_WS_CLIENT_H
#define IM_WS_CLIENT_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

bool im_ws_start(const char *host, uint16_t port, const char *ticket);
void im_ws_stop(void);
bool im_ws_is_connected(void);
uint32_t im_ws_last_pong_ms(void);

/* Send HEARTBEAT_PING when 30 s elapsed since the previous one. Returns
 * false when the send itself failed (caller should reconnect). */
bool im_ws_poll_heartbeat(uint32_t now_ms);

/* Send one CHAT/PRIVATE_CHAT frame; oversized content is shortened by
 * im_proto so the frame never exceeds 256 bytes. */
bool im_ws_send_chat(const char *to_uid, const char *content,
                     const char *stanza_id);

#endif /* IM_WS_CLIENT_H */
