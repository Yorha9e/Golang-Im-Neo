/* roundtrip_test.c — Host verification for nanopb WsMessage bindings.
 *
 * Covers MISSION M1 3a..3e:
 *  a. max-bound encode/decode round-trip, field-by-field equality.
 *  b. proto_WsMessage_size / sizeof fits ESP32 budget (< 1400 bytes).
 *  c. static-bound proof: over-long content (>512) decode FAILS cleanly.
 *  d. zero-malloc proof: field table contains only PB_ATYPE_STATIC.
 *  e. MsgType round-trip (HEARTBEAT_PING).
 *
 * Build (from worktree root):
 *   gcc -std=c99 -Wall -Wextra -Werror -I proto/nanopb \
 *     proto/nanopb/pb_common.c proto/nanopb/pb_encode.c \
 *     proto/nanopb/pb_decode.c proto/nanopb/message.pb.c \
 *     proto/nanopb/test/roundtrip_test.c -o /tmp/nanopb_roundtrip_test.exe
 *
 * Exit 0 on all PASS, 1 on any FAIL.
 */
#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include <stdbool.h>

#include "pb.h"
#include "pb_common.h"
#include "pb_encode.h"
#include "pb_decode.h"
#include "message.pb.h"

#define TEST_BUF_SIZE 2048U
#define ESP32_BUDGET_BYTES 1400U

static void fill_str(char *dst, size_t dst_size, char ch)
{
    size_t i;
    if (dst_size == 0U) {
        return;
    }
    for (i = 0U; i + 1U < dst_size; i++) {
        dst[i] = ch;
    }
    dst[dst_size - 1U] = '\0';
}

int main(void)
{
    int failures = 0;

    /* ---- (a) max-bound round-trip ---- */
    {
        proto_WsMessage msg = proto_WsMessage_init_zero;
        proto_WsMessage got = proto_WsMessage_init_zero;
        uint8_t buf[TEST_BUF_SIZE];
        pb_ostream_t os;
        pb_istream_t is;
        size_t i;
        bool ok;

        msg.type = proto_MsgType_PRIVATE_CHAT;
        msg.seq = 123456789012345LL;
        msg.timestamp = 1720000000000LL;
        fill_str(msg.from_uid, sizeof(msg.from_uid), 'F');
        fill_str(msg.to_uid, sizeof(msg.to_uid), 'T');
        fill_str(msg.content, sizeof(msg.content), 'C');
        fill_str(msg.extra, sizeof(msg.extra), 'E');
        fill_str(msg.stanza_id, sizeof(msg.stanza_id), 'S');
        msg.payload.size = (pb_size_t)sizeof(msg.payload.bytes);
        for (i = 0U; i < sizeof(msg.payload.bytes); i++) {
            msg.payload.bytes[i] = (pb_byte_t)(i & 0xFFU);
        }

        os = pb_ostream_from_buffer(buf, sizeof(buf));
        ok = pb_encode(&os, proto_WsMessage_fields, &msg);
        if (!ok) {
            printf("[FAIL] a: encode failed: %s\n", PB_GET_ERROR(&os));
            failures++;
        } else {
            is = pb_istream_from_buffer(buf, os.bytes_written);
            ok = pb_decode(&is, proto_WsMessage_fields, &got);
            if (!ok) {
                printf("[FAIL] a: decode failed: %s\n", PB_GET_ERROR(&is));
                failures++;
            } else {
                bool same = true;
                if (got.type != msg.type) { same = false; }
                if (got.seq != msg.seq) { same = false; }
                if (got.timestamp != msg.timestamp) { same = false; }
                if (strcmp(got.from_uid, msg.from_uid) != 0) { same = false; }
                if (strcmp(got.to_uid, msg.to_uid) != 0) { same = false; }
                if (strcmp(got.content, msg.content) != 0) { same = false; }
                if (strcmp(got.extra, msg.extra) != 0) { same = false; }
                if (strcmp(got.stanza_id, msg.stanza_id) != 0) { same = false; }
                if (got.payload.size != msg.payload.size) { same = false; }
                if (memcmp(got.payload.bytes, msg.payload.bytes,
                           sizeof(msg.payload.bytes)) != 0) { same = false; }
                if (same) {
                    printf("[PASS] a: max-bound round-trip ok (%u encoded bytes)\n",
                           (unsigned)os.bytes_written);
                } else {
                    printf("[FAIL] a: field mismatch after round-trip\n");
                    failures++;
                }
            }
        }
    }

    /* ---- (b) ESP32 budget ---- */
    {
        unsigned gen_size = (unsigned)proto_WsMessage_size;
        unsigned struct_size = (unsigned)sizeof(proto_WsMessage);
        printf("[INFO] b: proto_WsMessage_size=%u sizeof=%u budget=%u\n",
               gen_size, struct_size, (unsigned)ESP32_BUDGET_BYTES);
        if (gen_size < ESP32_BUDGET_BYTES && struct_size < ESP32_BUDGET_BYTES) {
            printf("[PASS] b: fits ESP32 budget\n");
        } else {
            printf("[FAIL] b: exceeds ESP32 budget\n");
            failures++;
        }
    }

    /* ---- (c) static-bound proof: over-long content must fail ---- */
    {
        /* field 5 (content), wire-type LEN: tag = (5<<3)|2 = 42 = 0x2A.
         * Length 600 varint = 0xD8 0x04, then 600 'X' bytes. */
        uint8_t evil[700];
        size_t pos = 0U;
        size_t k;
        proto_WsMessage evil_msg = proto_WsMessage_init_zero;
        pb_istream_t eis;
        bool ok;
        const char *err;

        evil[pos++] = 0x2AU;
        evil[pos++] = 0xD8U;
        evil[pos++] = 0x04U;
        for (k = 0U; k < 600U; k++) {
            evil[pos++] = (uint8_t)'X';
        }

        eis = pb_istream_from_buffer(evil, pos);
        ok = pb_decode(&eis, proto_WsMessage_fields, &evil_msg);
        err = PB_GET_ERROR(&eis);
        if (!ok) {
            printf("[PASS] c: over-long content rejected (%s)\n",
                   err ? err : "(null)");
            if (err == NULL || err[0] == '\0') {
                printf("[FAIL] c: PB_GET_ERROR empty\n");
                failures++;
            }
        } else {
            printf("[FAIL] c: over-long content accepted (must fail)\n");
            failures++;
        }
    }

    /* ---- (d) zero-malloc proof: all fields STATIC ---- */
    {
        proto_WsMessage dummy = proto_WsMessage_init_zero;
        pb_field_iter_t iter;
        bool begun;
        bool all_static = true;
        int nfields = 0;

        begun = pb_field_iter_begin_const(&iter, proto_WsMessage_fields, &dummy);
        if (!begun) {
            printf("[FAIL] d: field iterator begin failed\n");
            failures++;
        } else {
            do {
                if (PB_ATYPE(iter.type) != PB_ATYPE_STATIC) {
                    printf("[FAIL] d: tag %u atype 0x%x not STATIC\n",
                           (unsigned)iter.tag, (unsigned)PB_ATYPE(iter.type));
                    all_static = false;
                }
                nfields++;
            } while (pb_field_iter_next(&iter));
            if (all_static && nfields == 9) {
                printf("[PASS] d: %d fields all STATIC (no POINTER/CALLBACK)\n",
                       nfields);
            } else if (all_static) {
                printf("[FAIL] d: expected 9 fields, saw %d\n", nfields);
                failures++;
            } else {
                failures++;
            }
        }
    }

    /* ---- (e) MsgType round-trip ---- */
    {
        proto_WsMessage ping = proto_WsMessage_init_zero;
        proto_WsMessage pong = proto_WsMessage_init_zero;
        uint8_t buf[128];
        pb_ostream_t os;
        pb_istream_t is;
        bool ok;

        ping.type = proto_MsgType_HEARTBEAT_PING;
        ping.seq = 7LL;
        fill_str(ping.from_uid, sizeof(ping.from_uid), 'a');

        os = pb_ostream_from_buffer(buf, sizeof(buf));
        ok = pb_encode(&os, proto_WsMessage_fields, &ping);
        if (!ok) {
            printf("[FAIL] e: encode failed: %s\n", PB_GET_ERROR(&os));
            failures++;
        } else {
            is = pb_istream_from_buffer(buf, os.bytes_written);
            ok = pb_decode(&is, proto_WsMessage_fields, &pong);
            if (!ok) {
                printf("[FAIL] e: decode failed: %s\n", PB_GET_ERROR(&is));
                failures++;
            } else if (pong.type == proto_MsgType_HEARTBEAT_PING &&
                       pong.type == 1 && ping.type == 1) {
                printf("[PASS] e: MsgType HEARTBEAT_PING round-trip ok\n");
            } else {
                printf("[FAIL] e: type mismatch got=%d want=1\n",
                       (int)pong.type);
                failures++;
            }
        }
    }

    if (failures == 0) {
        printf("ALL TESTS PASSED\n");
        return 0;
    }
    printf("%d TEST(S) FAILED\n", failures);
    return 1;
}
