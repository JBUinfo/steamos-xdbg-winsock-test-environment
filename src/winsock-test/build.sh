#!/usr/bin/env bash

set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
OUT=$(cd -- "$ROOT/../.." && pwd -P)/examples
mkdir -p "$OUT"

build_pair() {
    local arch=$1 label=$2
    (
        cd -- "$ROOT"
        GOOS=windows GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "$OUT/winsock${label}-server.exe" ./cmd/server
        GOOS=windows GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "$OUT/winsock${label}-client.exe" ./cmd/client
    )
}

build_pair 386 32
build_pair amd64 64
printf 'Fixture binaries written to %s\n' "$OUT"
