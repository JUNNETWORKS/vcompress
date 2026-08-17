#!/usr/bin/env bash
# Cross-compile the binaries published by the release job in .github/workflows/ci.yml.
# Usage: scripts/build-dist.sh [output-dir]   (default: dist)
set -euo pipefail

out_dir="${1:-dist}"
targets=(linux/amd64 linux/arm64 windows/amd64 darwin/arm64)

mkdir -p "$out_dir"
for target in "${targets[@]}"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  ext=""
  if [ "$goos" = windows ]; then ext=".exe"; fi
  out="$out_dir/vcompress_${goos}_${goarch}${ext}"
  echo "building $out"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build -trimpath -ldflags '-s -w' -o "$out" ./cmd/vcompress
done
