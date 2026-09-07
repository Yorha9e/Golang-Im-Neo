#!/usr/bin/env bash
# run_host_test.sh — one-command host verification for M1 nanopb bindings.
# Usage (from worktree root):
#   bash proto/nanopb/test/run_host_test.sh
set -e
if [ ! -f proto/nanopb/message.pb.h ]; then
  echo "run from worktree root (proto/nanopb/message.pb.h not found)" >&2
  exit 1
fi
gcc -std=c99 -Wall -Wextra -Werror -I proto/nanopb \
  proto/nanopb/pb_common.c proto/nanopb/pb_encode.c \
  proto/nanopb/pb_decode.c proto/nanopb/message.pb.c \
  proto/nanopb/test/roundtrip_test.c -o /tmp/nanopb_roundtrip_test.exe
/tmp/nanopb_roundtrip_test.exe
