#!/usr/bin/env bash
# run_host_test.sh — one-command host verification for the M2 protocol layer.
# Usage (from worktree root):
#   bash esp32/host_test/run_host_test.sh
# Compiles esp32/main/im_proto.c against the shared proto/nanopb sources
# with gcc -std=c99 -Wall -Wextra -Werror, runs checks (a)..(d), then gates
# (e): im_proto.* must not reference heap calls (exit non-zero if found).
set -e
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
NANO="proto/nanopb"
gcc -std=c99 -Wall -Wextra -Werror -I "$NANO" -I esp32/main \
  "$NANO/pb_common.c" "$NANO/pb_encode.c" \
  "$NANO/pb_decode.c" "$NANO/message.pb.c" \
  esp32/main/im_proto.c esp32/host_test/host_test.c \
  -o /tmp/im_proto_host_test.exe
/tmp/im_proto_host_test.exe
if grep -nEw 'malloc|calloc|realloc|free|strdup' esp32/main/im_proto.c esp32/main/im_proto.h; then
  echo "[FAIL] e: heap call referenced in im_proto" >&2
  exit 1
fi
echo "[PASS] e: no heap calls in im_proto (zero-malloc)"
