/* im_proto.c — platform-neutral WsMessage frame codec, see im_proto.h.
 *
 * Depends only on nanopb and string.h / stdint.h / stdbool.h / stddef.h.
 * Portability notes for reviewers:
 *  - every buffer is caller-supplied (stack or static); this file defines
 *    no global state and performs no heap activity on any path;
 *  - strings are handled with explicit bounded loops (no locale, no
 *    multibyte libc); UTF-8 edges are found by byte inspection below;
 *  - the only stack scratch is a 256-byte probe in
 *    im_proto_max_content_for_frame; the build path reuses the caller
 *    output buffer for its probe, so steady-state stack use stays small.
 */

#include "im_proto.h"

#include <string.h>

#include "pb_decode.h"
#include "pb_encode.h"

/* Usable bytes per fixed C string (one byte kept back for NUL). */
#define IM_CONTENT_CAP \
    (sizeof(((proto_WsMessage *)0)->content) - 1u)

/* Wire headroom kept past the measured base message: content tag byte plus
 * length varint (1-2 bytes over our budgets) plus one byte of slack so the
 * first size guess normally fits without a second encode pass. */
#define IM_CONTENT_HEADROOM 4u

static void im_copy_cstr(char *dst, size_t dst_sz, const char *src)
{
    size_t i = 0u;
    if (dst == NULL || dst_sz == 0u) {
        return;
    }
    if (src != NULL) {
        while (i + 1u < dst_sz && src[i] != '\0') {
            dst[i] = src[i];
            i++;
        }
    }
    dst[i] = '\0';
}

static size_t im_cstr_bytes(const char *s)
{
    return (s == NULL) ? 0u : strlen(s);
}

/* Length of the UTF-8 sequence starting at lead byte c (1-4; a stray
 * continuation byte or any other odd value counts as 1 so the walk below
 * always advances and resyncs). */
static size_t im_utf8_seq_len(uint8_t c)
{
    if ((c & 0x80u) == 0u) {
        return 1u;
    }
    if ((c & 0xE0u) == 0xC0u) {
        return 2u;
    }
    if ((c & 0xF0u) == 0xE0u) {
        return 3u;
    }
    if ((c & 0xF8u) == 0xF0u) {
        return 4u;
    }
    return 1u;
}

/* Longest prefix of s (bytes, NUL not counted) fitting in max_bytes without
 * splitting a UTF-8 sequence. Malformed bytes pass through one at a time so
 * the walk always terminates with i <= max_bytes. */
static size_t im_rune_prefix_len(const char *s, size_t max_bytes)
{
    size_t i = 0u;
    if (s == NULL) {
        return 0u;
    }
    while (s[i] != '\0') {
        uint8_t c = (uint8_t)s[i];
        size_t want = im_utf8_seq_len(c);
        if (want == 1u) {
            if (i + 1u > max_bytes) {
                break;
            }
            i += 1u;
        } else {
            size_t k = 1u;
            if (i + want > max_bytes) {
                break;
            }
            while (k < want && s[i + k] != '\0' &&
                   (((uint8_t)s[i + k] & 0xC0u) == 0x80u)) {
                k++;
            }
            i += (k == want) ? want : 1u;
        }
    }
    return i;
}

static bool im_encode_msg(const proto_WsMessage *msg, uint8_t *out,
                          size_t out_cap, size_t *out_len)
{
    pb_ostream_t os;
    if (msg == NULL || out == NULL || out_cap == 0u) {
        return false;
    }
    os = pb_ostream_from_buffer(out, out_cap);
    if (!pb_encode(&os, proto_WsMessage_fields, msg)) {
        return false;
    }
    if (out_len != NULL) {
        *out_len = os.bytes_written;
    }
    return true;
}

static void im_copy_prefix(char *dst, size_t dst_sz, const char *src,
                           size_t keep)
{
    size_t i = 0u;
    if (dst == NULL || dst_sz == 0u) {
        return;
    }
    if (src == NULL) {
        dst[0] = '\0';
        return;
    }
    while (i < keep && i + 1u < dst_sz && src[i] != '\0') {
        dst[i] = src[i];
        i++;
    }
    dst[i] = '\0';
}

void im_proto_init_ping(proto_WsMessage *msg)
{
    if (msg == NULL) {
        return;
    }
    memset(msg, 0, sizeof(*msg));
    msg->type = proto_MsgType_HEARTBEAT_PING;
}

bool im_proto_build_ping(uint8_t *out, size_t out_cap, size_t *out_len)
{
    proto_WsMessage m = proto_WsMessage_init_zero;
    size_t n = 0u;
    m.type = proto_MsgType_HEARTBEAT_PING;
    if (!im_encode_msg(&m, out, out_cap, &n)) {
        return false;
    }
    if (n == 0u || n > IM_MAX_FRAME) {
        return false;
    }
    if (out_len != NULL) {
        *out_len = n;
    }
    return true;
}

size_t im_proto_max_content_for_frame(const char *to_uid,
                                      const char *stanza_id)
{
    proto_WsMessage base_msg = proto_WsMessage_init_zero;
    uint8_t probe[IM_MAX_FRAME];
    size_t n = 0u;
    size_t budget;
    base_msg.type = proto_MsgType_CHAT;
    im_copy_cstr(base_msg.to_uid, sizeof(base_msg.to_uid), to_uid);
    im_copy_cstr(base_msg.stanza_id, sizeof(base_msg.stanza_id), stanza_id);
    if (!im_encode_msg(&base_msg, probe, sizeof(probe), &n)) {
        return 0u;
    }
    if (n + IM_CONTENT_HEADROOM >= IM_MAX_FRAME) {
        return 0u;
    }
    budget = IM_MAX_FRAME - n - IM_CONTENT_HEADROOM;
    return (budget < IM_CONTENT_CAP) ? budget : IM_CONTENT_CAP;
}

im_proto_status_t im_proto_build_chat(proto_MsgType type,
                                      const char *to_uid,
                                      const char *content,
                                      const char *stanza_id,
                                      uint8_t *out,
                                      size_t out_cap,
                                      size_t *out_len)
{
    proto_WsMessage m = proto_WsMessage_init_zero;
    size_t base = 0u;
    size_t full = 0u;
    size_t budget = 0u;
    size_t keep = 0u;
    bool cut = false;
    size_t n = 0u;

    if (out == NULL || out_len == NULL || out_cap < IM_MAX_FRAME) {
        return IM_PROTO_ERR;
    }
    if (type != proto_MsgType_CHAT && type != proto_MsgType_PRIVATE_CHAT) {
        return IM_PROTO_ERR;
    }
    if (content == NULL) {
        content = "";
    }

    m.type = type;
    im_copy_cstr(m.to_uid, sizeof(m.to_uid), to_uid);
    im_copy_cstr(m.stanza_id, sizeof(m.stanza_id), stanza_id);

    /* Probe: encode without content into the caller buffer to learn the
     * fixed overhead, then size the content so the final frame fits. */
    if (!im_encode_msg(&m, out, out_cap, &base)) {
        return IM_PROTO_ERR;
    }
    if (base + IM_CONTENT_HEADROOM < IM_MAX_FRAME) {
        budget = IM_MAX_FRAME - base - IM_CONTENT_HEADROOM;
    } else {
        budget = 0u;
    }
    if (budget > IM_CONTENT_CAP) {
        budget = IM_CONTENT_CAP;
    }

    full = im_cstr_bytes(content);
    keep = im_rune_prefix_len(content, budget);
    cut = (keep < full);
    im_copy_prefix(m.content, sizeof(m.content), content, keep);

    if (!im_encode_msg(&m, out, out_cap, &n)) {
        return IM_PROTO_ERR;
    }
    /* Fix-up: the length varint can cost one byte more than the headroom
     * once content crosses 127 bytes. Each pass strictly shortens the
     * frame (length-delimited field: fewer payload bytes always encode
     * smaller) and the empty-content probe above already fits, so this
     * always terminates with room. */
    while (n > IM_MAX_FRAME && keep > 0u) {
        keep = im_rune_prefix_len(content, keep - 1u);
        cut = true;
        im_copy_prefix(m.content, sizeof(m.content), content, keep);
        if (!im_encode_msg(&m, out, out_cap, &n)) {
            return IM_PROTO_ERR;
        }
    }
    if (n > IM_MAX_FRAME) {
        return IM_PROTO_ERR;
    }
    *out_len = n;
    return cut ? IM_PROTO_TRUNCATED : IM_PROTO_OK;
}

bool im_proto_decode(const uint8_t *data, size_t len, proto_WsMessage *out)
{
    pb_istream_t is;
    if (data == NULL || out == NULL || len == 0u) {
        return false;
    }
    memset(out, 0, sizeof(*out));
    is = pb_istream_from_buffer(data, len);
    return pb_decode(&is, proto_WsMessage_fields, out);
}

proto_MsgType im_proto_classify(const proto_WsMessage *msg)
{
    if (msg == NULL) {
        return proto_MsgType_UNKNOWN;
    }
    return msg->type;
}
