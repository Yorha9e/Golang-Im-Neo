/* im_proto.h — platform-neutral WsMessage frame layer for the ESP32-C3 client.
 *
 * Depends only on nanopb (message.pb.h + pb_encode/pb_decode) and the C
 * headers stdint.h / stdbool.h / stddef.h / string.h. No RTOS, no ESP-IDF,
 * no socket includes, so this unit builds as-is on the host with
 *   gcc -std=c99 -Wall -Wextra -Werror
 * (see esp32/host_test/).
 *
 * Buffer discipline:
 *  - every buffer is caller-supplied (stack or static storage); this layer
 *    keeps no state of its own and never touches the heap on any path;
 *  - outbound frames never exceed IM_MAX_FRAME (256) bytes: the chat
 *    builders shrink content on a UTF-8 rune edge until the encoded frame
 *    fits, and report whether they had to shrink;
 *  - inbound decode targets a caller struct; oversized wire data (the
 *    server may deliver up to ~1 KB envelopes to any client) and malformed
 *    input fail closed with false and leave the struct zeroed.
 */

#ifndef IM_PROTO_H
#define IM_PROTO_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "message.pb.h"

#ifdef __cplusplus
extern "C" {
#endif

/* Outbound discipline: no emitted frame is ever larger than this. Mirrors
 * the server gateway hardware threshold (internal/gateway/truncate.go). */
#define IM_MAX_FRAME 256u

/* Heartbeat cadence: client sends HEARTBEAT_PING this often; the server
 * answers HEARTBEAT_PONG (see internal/router/router.go dispatch). */
#define IM_HEARTBEAT_PERIOD_MS 30000u

/* Build outcome for chat frames. */
typedef enum im_proto_status {
    IM_PROTO_OK = 0,        /* frame built, content untouched */
    IM_PROTO_TRUNCATED = 1, /* frame built, content shortened to fit 256 B */
    IM_PROTO_ERR = 2        /* bad argument or encode failure, no output */
} im_proto_status_t;

/* Fill msg as a HEARTBEAT_PING (all other fields zero). */
void im_proto_init_ping(proto_WsMessage *msg);

/* Encode a HEARTBEAT_PING into out[0..out_cap). Returns true with *out_len
 * set (always <= IM_MAX_FRAME) on success. */
bool im_proto_build_ping(uint8_t *out, size_t out_cap, size_t *out_len);

/* Conservative content budget (bytes) for a chat frame carrying the given
 * to_uid / stanza_id such that the encoded frame still fits IM_MAX_FRAME.
 * The builders already enforce the cap exactly; use this to size UI input. */
size_t im_proto_max_content_for_frame(const char *to_uid,
                                      const char *stanza_id);

/* Encode a CHAT or PRIVATE_CHAT frame (client sends seq 0; the server stamps
 * seq/timestamp and overwrites from_uid). to_uid/content/stanza_id may be
 * NULL (= ""). Content longer than the frame budget is shortened on a UTF-8
 * rune edge and reported as IM_PROTO_TRUNCATED; any other type, a NULL out,
 * or out_cap < IM_MAX_FRAME yields IM_PROTO_ERR. On success *out_len holds
 * the frame size, always <= IM_MAX_FRAME. */
im_proto_status_t im_proto_build_chat(proto_MsgType type,
                                      const char *to_uid,
                                      const char *content,
                                      const char *stanza_id,
                                      uint8_t *out,
                                      size_t out_cap,
                                      size_t *out_len);

/* Decode one binary WS frame into the caller struct (zeroed first, so a
 * false return always pairs with a zeroed struct). */
bool im_proto_decode(const uint8_t *data, size_t len, proto_WsMessage *out);

/* Read the envelope type (UNKNOWN when msg is NULL). */
proto_MsgType im_proto_classify(const proto_WsMessage *msg);

#ifdef __cplusplus
}
#endif

#endif /* IM_PROTO_H */
