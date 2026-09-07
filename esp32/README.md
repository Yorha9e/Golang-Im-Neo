# ESP32-C3 IM Client (M2 firmware prototype)

ESP-IDF project, target **ESP32-C3**, implementing the hardware-class client
of the Golang IM Neo System: WiFi STA → HTTP login (`device_class
"hardware"`) → WS ticket → binary-protobuf WebSocket (`/ws?ticket=...`) →
30 s heartbeat, with a platform-neutral zero-malloc protocol layer shared
with the host test.

## 1. Toolchain

- ESP-IDF **v5.x** (`$IDF_PATH` exported, tools on PATH). No IDF on the
  dev host is fine — Phase 1 proves compile-readiness by structure +
  host-tested core + review.
- Host verification only needs `gcc` (TDM-GCC-64 10.3.0) + git-bash.

## 2. Build / flash / monitor

```bash
cd esp32
idf.py set-target esp32c3
idf.py menuconfig   # see §3, "ESP32 IM Client"
idf.py build
idf.py -p COMx flash monitor
```

`components/nanopb` compiles the shared `proto/nanopb` sources
(`pb_common/pb_encode/pb_decode/message.pb`) **by relative path — no
duplication**. `proto/nanopb` stays the single source of truth.

## 3. menuconfig fields (`main/Kconfig.projbuild`)

| Symbol | Default | Meaning |
|---|---|---|
| `ESP_IM_WIFI_SSID` / `ESP_IM_WIFI_PASSWORD` | `test-ap` / `test-password` | STA credentials |
| `ESP_IM_SERVER_HOST` | `192.168.1.100` | Bare host (no scheme) for `http://` + `ws://` |
| `ESP_IM_SERVER_PORT` | `8080` | Go server port (`config.toml port`) |
| `ESP_IM_AUTH_USERNAME` / `ESP_IM_AUTH_PASSWORD` | `esp32c3-01` / `esp32-pass` | Login identity |

## 4. Message-flow wiring

```
app_main.c (link task, 1 s tick)
  │  NVS → im_wifi_start() ──STA/DHCP──▶ GOT_IP
  ▼
POST /api/v1/auth/login {"username","password",
      "device_class":"hardware","device_name":"esp32c3-01"}
  envelope {code,msg,data:{access_token,user_id,session_id}}
  ▼
POST /api/v1/auth/ticket  Authorization: Bearer <at>
  envelope {data:{ticket}}            (ticket zeroed after use)
  ▼
im_ws_client.c ── ws://host:port/ws?ticket=<url-encoded> ──▶ server
  │  esp_websocket_client, binary frames only (op_code 2)
  │  RX: static s_rx[1400] reassembly by payload_offset → im_proto_decode
  │       into static proto_WsMessage → classify:
  │       PONG → pong timestamp · PRIVATE_CHAT → log + capped canned reply
  │       CHAT/NOTICE/ACK → log · else → drop
  │  TX: static s_tx[256]; im_proto builders guarantee ≤ 256 B,
  │       im_send_raw() re-checks (defence in depth)
  ▼
im_proto.c (platform-neutral: nanopb + string.h only, no heap, no IDF)
  build_ping / build_chat (auto-shorten on UTF-8 edge) /
  max_content_for_frame / decode / classify
```

Outbound discipline: **no emitted frame exceeds 256 bytes** — the builder
probes the fixed overhead, budgets content, and fix-up-shrinks rune by rune
until the encoded frame fits (status `IM_PROTO_TRUNCATED`, send path logs
and still sends). Inbound is roomier on purpose: a server-truncated
delivery still carries its full envelope (measured **297 wire bytes** in the
host test), so RX is 1400 B while TX is 256 B.

## 5. Host test (no hardware needed)

From the worktree root:

```bash
bash esp32/host_test/run_host_test.sh
```

Expected output (TDM-GCC 10.3.0, zero warnings under
`-std=c99 -Wall -Wextra -Werror`):

```text
[PASS] a: ping builds within 256 B
[INFO] a: ping frame = 2 bytes
[PASS] a: ping round-trip type == HEARTBEAT_PING
[PASS] b: CHAT builds untouched within 256 B
[PASS] b: CHAT round-trip keeps content + stanza_id
[PASS] b: PRIVATE_CHAT builds untouched within 256 B
[PASS] b: PRIVATE_CHAT round-trip keeps to_uid + content
[PASS] b: budget helper returns a usable sub-frame bound
[INFO] b: max content for bare frame = 250 bytes
[PASS] b: budget-sized content fits without shortening
[PASS] c: 1000 B content auto-shortened, frame <= 256 B
[INFO] c: shortened frame = 255 bytes
[PASS] c: shortened content honours the per-frame budget
[PASS] c: shortening keeps a clean prefix
[PASS] d: fixture is a true 256 B server-style payload
[PASS] d: server-style frame encodes
[INFO] d: truncated wire frame = 297 bytes
[PASS] d: server-truncated frame decodes
[PASS] d: suffix intact, content 256 B
ALL TESTS PASSED
[PASS] e: no heap calls in im_proto (zero-malloc)
```

Checks: (a) heartbeat round-trip, (b) CHAT/PRIVATE_CHAT round-trips +
budget helper, (c) 1000 B content auto-shortened to ≤ 256 B (policy:
shorten, never reject), (d) server-style `...[长消息截断]` frame decodes
with suffix intact, (e) grep gate — no heap calls in `im_proto.*`.

## 6. 24 h-stability design notes

- **Reconnect backoff (two levels).** WiFi: `STA_DISCONNECTED` arms a
  one-shot timer at `min(2^n s, 60 s)`, reset on `GOT_IP`. Session: any
  link loss (WiFi down, WS down, heartbeat send failure, **90 s pong
  timeout**) tears everything down and re-runs the *full* chain
  (login → ticket → WS) after `1,2,4…60 s`. Full re-login (no token
  cache) tracks short server TTLs; tickets are single-use and zeroed ASAP.
- **Static buffers only.** `s_rx[1400]`, `s_tx[256]`, `s_msg` (~1 KB),
  `s_url[2048]`, token/response/body scratch — all static. The WS data
  path (`im_ws_client.c`, `#define IM_ZERO_MALLOC 1` + audit block) and
  `im_proto.c` perform no heap activity; oversize/ragged input is dropped
  before any copy. Largest stack transient is ~1.3 KB in the build path
  (message + 256 B probe); WS task stack is set to 6144, link task 6144.
- **Watchdog strategy.** No TWDT subscription games: the 1 s link-task
  tick *is* the app watchdog — heartbeat every 30 s, pong supervision at
  90 s (3 missed), immediate reconnect on send failure. A wedged network
  task therefore always surfaces as a session reset within ~90 s; IDF's
  default interrupt/task watchdogs still guard against true CPU lockup.
- **Heartbeat.** Tick-count scheduling in the link task
  (`IM_HEARTBEAT_PERIOD_MS = 30000`, `im_ws_poll_heartbeat()`); first ping
  goes out immediately on `WEBSOCKET_EVENT_CONNECTED`. Server answers
  `HEARTBEAT_PONG` (router `dispatch`), which refreshes the pong stamp.
- **Known limits (prototype).** Plain `ws://` + `http://` only (no TLS —
  Stage 4); demo traffic is one hello CHAT per connection plus canned
  PRIVATE_CHAT replies; JSON parsing is a flat `{"k":"v"}` extractor
  (matches today's auth envelope); `sdkconfig.defaults` flash pins
  (QIO/80 MHz/4 MB) assume a standard C3 module — adjust for your board.
