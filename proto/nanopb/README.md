# proto/nanopb — Nanopb C runtime + WsMessage bindings (M1)

## 1. Provenance

- Nanopb version: **0.4.9.1** (`NANOPB_VERSION "nanopb-0.4.9.1"` in `pb.h:68`).
- Runtime files (`pb.h`, `pb_common.h/.c`, `pb_encode.h/.c`, `pb_decode.h/.c`):
  upstream-verbatim from GitHub tag tarball:
  `https://github.com/nanopb/nanopb/archive/refs/tags/0.4.9.1.tar.gz`
  (files at tarball root `nanopb-0.4.9.1/pb*`).
- Why GitHub and not pip sdist: the PyPI sdist
  `https://files.pythonhosted.org/packages/85/84/09151cd854d3fc496206c1a58980a14577f30cd0f9f329afe1ac66db650a/nanopb-0.4.9.1.tar.gz`
  (`pip download nanopb==0.4.9.1 --no-deps --no-binary :all:`) contains
  **generator only** (23 files, no `pb*.c/h`). Verified 2026-09-07:
  `/tmp/nanopb-src/nanopb-0.4.9.1` has no C runtime. The GitHub tag tarball
  is the same upstream commit family (0.4.9.x) and its `pb*` match the
  generator version, so it is used for the runtime. Both archives were kept:
  pip sdist at `/tmp/nanopb-src/nanopb-0.4.9.1.tar.gz`, GitHub at
  `/tmp/nanopb-src/nanopb-github-0.4.9.1.tar.gz`.
- Runtime SHA256 (dst == src, verified with `sha256sum`):
  - `pb.h` `a2ecdca9fdaeef5f4972ed983540c0d6fb0a5c402a2e0b0349d7e1bc5e188d29`
  - `pb_common.h` `6495a691aca68d6973f2274b5dd54b74fbb57f6b019c45fff255a857fe1abcfd`
  - `pb_common.c` `8d2ec28baaaf2b7a5e90e4cb2fa9700d21cef7f826f051a637c30b7a1e6a0516`
  - `pb_encode.h` `9aa00fee4ff08adf0da16e33a55be08810ea657800a648dc78f82e89c60c10cf`
  - `pb_encode.c` `d8dff2a1acc58683095a41b0dc3103ba46248e4a8d8c4e20f5810be04127b650`
  - `pb_decode.h` `1747746e5961de5789bcf0795588da0790cd18b2e4e706ad9c7099a0fa1cc83f`
  - `pb_decode.c` `6c2fc2f357bffdb774c1d329b533e981498d58405c0b1ef066f5a87fd46b5a17`
- Bindings (`message.pb.h/.c`): **true generator output** (not hand-written),
  via `nanopb_generator.py` 0.4.9.1 (pip wheel `nanopb==0.4.9.1`) +
  `grpcio-tools` protoc (`libprotoc 35.1`, `protobuf 7.35.1`).
  `message.pb.h` is byte-identical to generator output; `message.pb.c`
  differs by exactly one line (include-path fix, see §2).

## 2. Regeneration (exact commands, Windows git-bash)

Setup (once):

```bash
pip install "nanopb==0.4.9.1" "grpcio-tools"
```

Generate (run from **worktree root** — cwd matters: the generator resolves
`proto/message.options` from the virtual path `proto/message.proto`):

```bash
WT="$(pwd)"
WTW=$(cygpath -w "$WT")
OUT="/tmp/nanopb-gen-wt"; rm -rf "$OUT"; mkdir -p "$OUT"
OUTW=$(cygpath -w "$OUT")
PLUGIN="C:\\Users\\Yorha\\AppData\\Roaming\\Python\\Python314\\Scripts\\protoc-gen-nanopb.exe"
cd "$WT" && python -m grpc_tools.protoc \
  "-I$WTW" \
  "--plugin=protoc-gen-nanopb=$PLUGIN" \
  "--nanopb_out=$OUTW" \
  "$WTW\\proto\\message.proto"
```

Then install (one-line include fix for the flat `proto/nanopb/` layout):

```bash
cp /tmp/nanopb-gen-wt/proto/message.pb.h proto/nanopb/message.pb.h
cp /tmp/nanopb-gen-wt/proto/message.pb.c proto/nanopb/message.pb.c
sed -i 's|#include "proto/message.pb.h"|#include "message.pb.h"|' \
  proto/nanopb/message.pb.c
diff -u /tmp/nanopb-gen-wt/proto/message.pb.c proto/nanopb/message.pb.c
# expect only the #include line differs; .h must be identical
```

Notes:

- `-I` must be the worktree root with file `proto/message.proto`.
  Using `-I proto` with file `message.proto` silently **ignores**
  `message.options` (all fields become `CALLBACK`); the correct invocation
  above yields 9× `STATIC`.
- Contract respected (`proto/message.options`):
  `from_uid[36]`, `to_uid[36]`, `content[512]`, `payload PB_BYTES_ARRAY_T(256)`,
  `extra[128]`, `stanza_id[64]`, `proto_WsMessage_size 1066`,
  `PB_BIND(proto_WsMessage, proto_WsMessage, 2)`, zero `pb_callback_t`.
  Nanopb `max_size:N` = C array size `N` (room for NUL included).

## 3. Host test

One command (from worktree root, git-bash):

```bash
bash proto/nanopb/test/run_host_test.sh
```

It runs:

```bash
gcc -std=c99 -Wall -Wextra -Werror -I proto/nanopb \
  proto/nanopb/pb_common.c proto/nanopb/pb_encode.c \
  proto/nanopb/pb_decode.c proto/nanopb/message.pb.c \
  proto/nanopb/test/roundtrip_test.c -o /tmp/nanopb_roundtrip_test.exe
/tmp/nanopb_roundtrip_test.exe
```

Expected output (2026-09-07, TDM-GCC 10.3.0, zero warnings):

```text
[PASS] a: max-bound round-trip ok (1058 encoded bytes)
[INFO] b: proto_WsMessage_size=1066 sizeof=1064 budget=1400
[PASS] b: fits ESP32 budget
[PASS] c: over-long content rejected (string overflow)
[PASS] d: 9 fields all STATIC (no POINTER/CALLBACK)
[PASS] e: MsgType HEARTBEAT_PING round-trip ok
ALL TESTS PASSED
```

Test logic (`proto/nanopb/test/roundtrip_test.c`):

- (a) fills every string to `sizeof-1` (`F×35`, `T×35`, `C×511`, `E×127`,
  `S×63`) + payload 256 bytes `0..255`, encodes, decodes, `strcmp`/`memcmp`
  field-by-field.
- (b) asserts `proto_WsMessage_size < 1400` and `sizeof < 1400`.
- (c) hand-crafts tag `0x2A` + varint `0xD8 0x04` (len 600) + 600 `X`,
  asserts `pb_decode` returns false with `PB_GET_ERROR` = `string overflow`.
- (d) iterates with `pb_field_iter_begin_const`/`pb_field_iter_next`,
  asserts `PB_ATYPE(iter.type) == PB_ATYPE_STATIC` for all 9 fields
  (stack-only; no `POINTER`/`CALLBACK`). Documents zero-malloc by
  construction; no heap call exists in the decode path.
- (e) encodes `HEARTBEAT_PING=1`, decodes, asserts equality.

## 4. ESP-IDF / Arduino integration

Add to your component (no extra defines required for default static build):

```text
proto/nanopb/pb_common.c
proto/nanopb/pb_encode.c
proto/nanopb/pb_decode.c
proto/nanopb/message.pb.c
```

Include path: `proto/nanopb` (for `#include <pb.h>` and `#include "message.pb.h"`).
Example:

```c
#include "message.pb.h"
#include "pb_encode.h"
#include "pb_decode.h"

proto_WsMessage m = proto_WsMessage_init_zero;
uint8_t buf[proto_WsMessage_size];
pb_ostream_t os = pb_ostream_from_buffer(buf, sizeof(buf));
pb_encode(&os, proto_WsMessage_fields, &m);
```

Notes:

- No `malloc`/`pb_callback_t`: all fields are fixed-size; decode is
  stack-only — suitable for ESP32-C3 (400KB SRAM).
- `PB_BYTES_ARRAY_T(256)`: set `msg.payload.size` (0..256) before encode.
- Strings are NUL-terminated C arrays; max usable chars = array size − 1.
- No `PB_BUFFER_ONLY`/`PB_NO_ERRMSG` defines needed; defaults are fine.
  For tighter flash, `PB_NO_ERRMSG` may be defined (test (c) message changes).
