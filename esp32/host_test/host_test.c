/* host_test.c — host verification for esp32/main/im_proto.c.
 *
 * Builds with the shared nanopb sources (no ESP-IDF):
 *   gcc -std=c99 -Wall -Wextra -Werror -I proto/nanopb -I esp32/main ...
 * Covers MISSION M2 §2 a..d; check (e) (no heap calls in im_proto) runs as
 * a grep gate in run_host_test.sh. Exit 0 when every check passes.
 */
#include <stdio.h>
#include <string.h>

#include "im_proto.h"
#include "pb_decode.h"
#include "pb_encode.h"

/* Server truncation suffix (internal/gateway/truncate.go TruncateSuffix),
 * 20 bytes on the wire: 3 + 1 + 15 (5 CJK x 3 B) + 1. */
static const char kTruncateSuffix[] = "...[长消息截断]";

static int s_fail = 0;

static void check(int ok, const char *name, const char *detail)
{
    printf("[%s] %s: %s\n", ok ? "PASS" : "FAIL", name, detail);
    if (!ok) {
        s_fail++;
    }
}

/* a. heartbeat frame: encode -> decode -> type == HEARTBEAT_PING. */
static void test_heartbeat(void)
{
    uint8_t frame[IM_MAX_FRAME];
    size_t n = 0u;
    proto_WsMessage got;
    int built = im_proto_build_ping(frame, sizeof(frame), &n);
    check(built && n > 0u && n <= IM_MAX_FRAME, "a",
          "ping builds within 256 B");
    printf("[INFO] a: ping frame = %u bytes\n", (unsigned)n);
    check(im_proto_decode(frame, n, &got) &&
              im_proto_classify(&got) == proto_MsgType_HEARTBEAT_PING,
          "a", "ping round-trip type == HEARTBEAT_PING");
}

/* b. CHAT round-trip with stanza_id; PRIVATE_CHAT with to_uid round-trip. */
static void test_chat_roundtrip(void)
{
    uint8_t frame[IM_MAX_FRAME];
    size_t n = 0u;
    proto_WsMessage got;
    im_proto_status_t st;

    st = im_proto_build_chat(proto_MsgType_CHAT, "", "hello esp32",
                             "stanza-001", frame, sizeof(frame), &n);
    check(st == IM_PROTO_OK && n <= IM_MAX_FRAME, "b",
          "CHAT builds untouched within 256 B");
    check(im_proto_decode(frame, n, &got) &&
              im_proto_classify(&got) == proto_MsgType_CHAT &&
              strcmp(got.content, "hello esp32") == 0 &&
              strcmp(got.stanza_id, "stanza-001") == 0,
          "b", "CHAT round-trip keeps content + stanza_id");

    st = im_proto_build_chat(proto_MsgType_PRIVATE_CHAT, "user-b-uid",
                             "hi b", "stanza-002", frame, sizeof(frame), &n);
    check(st == IM_PROTO_OK && n <= IM_MAX_FRAME, "b",
          "PRIVATE_CHAT builds untouched within 256 B");
    check(im_proto_decode(frame, n, &got) &&
              im_proto_classify(&got) == proto_MsgType_PRIVATE_CHAT &&
              strcmp(got.to_uid, "user-b-uid") == 0 &&
              strcmp(got.content, "hi b") == 0,
          "b", "PRIVATE_CHAT round-trip keeps to_uid + content");
}

/* Budget helper sanity: nonzero, below the frame cap, and content sized
 * exactly to it encodes first-try without shortening. */
static void test_budget(void)
{
    size_t b0 = im_proto_max_content_for_frame("", "");
    uint8_t frame[IM_MAX_FRAME];
    static char exact[256];
    size_t n = 0u;
    im_proto_status_t st;
    check(b0 > 0u && b0 < IM_MAX_FRAME, "b",
          "budget helper returns a usable sub-frame bound");
    printf("[INFO] b: max content for bare frame = %u bytes\n",
           (unsigned)b0);
    if (b0 < sizeof(exact)) {
        size_t i;
        for (i = 0u; i < b0; i++) {
            exact[i] = 'q';
        }
        exact[b0] = '\0';
        st = im_proto_build_chat(proto_MsgType_CHAT, "", exact, "", frame,
                                 sizeof(frame), &n);
        check(st == IM_PROTO_OK && n <= IM_MAX_FRAME, "b",
              "budget-sized content fits without shortening");
    }
}

/* c. outbound cap: a 1000-byte CHAT is shortened by the layer (TRUNCATED)
 * and the emitted frame still fits 256 B. Policy: auto-shorten, never
 * reject — the send path must stay total (drop nothing silently). */
static void test_outbound_cap(void)
{
    uint8_t frame[IM_MAX_FRAME];
    static char big[1001];
    size_t n = 0u;
    proto_WsMessage got;
    im_proto_status_t st;
    size_t i;
    size_t budget;
    memset(big, 'x', sizeof(big) - 1u);
    big[sizeof(big) - 1u] = '\0';
    st = im_proto_build_chat(proto_MsgType_CHAT, "peer", big, "stanza-big",
                             frame, sizeof(frame), &n);
    check(st == IM_PROTO_TRUNCATED && n <= IM_MAX_FRAME, "c",
          "1000 B content auto-shortened, frame <= 256 B");
    printf("[INFO] c: shortened frame = %u bytes\n", (unsigned)n);
    budget = im_proto_max_content_for_frame("peer", "stanza-big");
    check(im_proto_decode(frame, n, &got) && strlen(got.content) <= budget,
          "c", "shortened content honours the per-frame budget");
    for (i = 0u; i < strlen(got.content); i++) {
        if (got.content[i] != 'x') {
            break;
        }
    }
    check(i == strlen(got.content), "c", "shortening keeps a clean prefix");
}

/* d. server-truncated inbound: content shaped like the gateway adapter
 * (236 B prefix on a rune edge + 20 B suffix = 256 B content inside a
 * larger envelope) decodes fine and keeps the suffix intact. */
static void test_server_truncated(void)
{
    static char content[300];
    uint8_t wire[proto_WsMessage_size];
    proto_WsMessage inm = proto_WsMessage_init_zero;
    proto_WsMessage got;
    pb_ostream_t os;
    size_t prefix = 0u;
    size_t clen;
    size_t tail;
    /* 101 ASCII + 45 CJK (135 B) = 236 B prefix, mirroring the server
     * prefix budget (256 - 20 suffix). */
    memset(content, 'a', 101u);
    prefix = 101u;
    while (prefix + 3u <= 236u) {
        content[prefix] = (char)0xE4;
        content[prefix + 1u] = (char)0xB8;
        content[prefix + 2u] = (char)0xAD; /* U+4E2D */
        prefix += 3u;
    }
    memcpy(content + prefix, kTruncateSuffix, strlen(kTruncateSuffix));
    clen = prefix + strlen(kTruncateSuffix);
    content[clen] = '\0';

    check(strlen(kTruncateSuffix) == 20u && prefix == 236u && clen == 256u,
          "d", "fixture is a true 256 B server-style payload");
    inm.type = proto_MsgType_CHAT;
    inm.seq = 987;
    inm.timestamp = 1725000000000LL;
    strcpy(inm.from_uid, "user-alice");
    strcpy(inm.content, content);
    strcpy(inm.stanza_id, "srv-stanza-9");
    os = pb_ostream_from_buffer(wire, sizeof(wire));
    check(pb_encode(&os, proto_WsMessage_fields, &inm), "d",
          "server-style frame encodes");
    printf("[INFO] d: truncated wire frame = %u bytes\n",
           (unsigned)os.bytes_written);
    check(im_proto_decode(wire, os.bytes_written, &got) &&
              im_proto_classify(&got) == proto_MsgType_CHAT,
          "d", "server-truncated frame decodes");
    tail = strlen(got.content) - strlen(kTruncateSuffix);
    check(strlen(got.content) == 256u &&
              strcmp(got.content + tail, kTruncateSuffix) == 0,
          "d", "suffix intact, content 256 B");
}

int main(void)
{
    test_heartbeat();
    test_chat_roundtrip();
    test_budget();
    test_outbound_cap();
    test_server_truncated();
    if (s_fail == 0) {
        printf("ALL TESTS PASSED\n");
        return 0;
    }
    printf("%d CHECK(S) FAILED\n", s_fail);
    return 1;
}
