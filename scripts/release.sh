#!/bin/sh
set -eu
mkdir -p bin
for platform in darwin/arm64 darwin/amd64 windows/amd64 windows/arm64 linux/amd64 linux/arm64; do
  os=${platform%/*}
  architecture=${platform#*/}
  file="classrelay-$os-$architecture"
  if [ "$os" = windows ]; then file="$file.exe"; fi
  CGO_ENABLED=0 GOOS="$os" GOARCH="$architecture" go build -trimpath -o "bin/$file" ./cmd/classrelay
done
