#!/usr/bin/env bash

set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
LIB="$ROOT/lib"
MINHOOK="$ROOT/vendor/minhook"

MINGW_ROOTS=(
    "${MINGW_ROOT:-}"
    "/home/deck/.local/share/lxdbg-mingw-root"
    "/tmp/mingw-root"
)

find_cc() {
    local name=$1
    local root
    command -v "$name" 2>/dev/null || {
        for root in "${MINGW_ROOTS[@]}"; do
            [[ -n "$root" && -x "$root/usr/bin/$name" ]] || continue
            printf '%s' "$root/usr/bin/$name"
            return 0
        done
    }
}

cc64=${MINGW64_CC:-$(find_cc x86_64-w64-mingw32-gcc)}
cc32=${MINGW32_CC:-$(find_cc i686-w64-mingw32-gcc)}
[[ -n "$cc64" && -x "$cc64" ]] || {
    printf 'Missing x86_64-w64-mingw32-gcc.\n' >&2
    exit 1
}
[[ -n "$cc32" && -x "$cc32" ]] || {
    printf 'Missing i686-w64-mingw32-gcc.\n' >&2
    exit 1
}

mkdir -p "$LIB"

build_one() {
    local cc=$1
    local label=$2
    local hde=$3

    printf 'Building ws2-hook%s.dll...\n' "$label"
    "$cc" -shared -O2 -Wall -Wextra \
        -D_WIN32_WINNT=0x0601 \
        -I"$MINHOOK/include" -I"$MINHOOK/src" \
        -o "$LIB/ws2-hook${label}.dll" \
        "$ROOT/src/ws2_hook.c" \
        "$MINHOOK/src/buffer.c" "$MINHOOK/src/hook.c" \
        "$MINHOOK/src/trampoline.c" "$MINHOOK/src/hde/$hde.c" \
        -lws2_32 -static-libgcc
}

build_one "$cc64" 64 hde64
build_one "$cc32" 32 hde32
printf 'DLLs written to %s\n' "$LIB"
