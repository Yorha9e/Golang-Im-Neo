# components/nanopb/CMakeLists.txt — Nanopb component wrapper.
#
# Wraps the shared M1 sources at proto/nanopb/ by relative path; no file
# duplication. The generated message.pb.c + runtime (pb_common/pb_encode/
# pb_decode) are compiled once here, and proto/nanopb stays the single
# source of truth for both firmware and host tests.
set(NANOPB_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../../../proto/nanopb")

idf_component_register(
    SRCS
        "${NANOPB_DIR}/pb_common.c"
        "${NANOPB_DIR}/pb_encode.c"
        "${NANOPB_DIR}/pb_decode.c"
        "${NANOPB_DIR}/message.pb.c"
    INCLUDE_DIRS
        "."
        "${NANOPB_DIR}"
)
