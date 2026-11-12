#!/usr/bin/env bash
# Release build for gh-extension-precompile: macOS and Linux only, since
# run needs rsync and ssh ControlMaster. The action passes the tag as $1.
set -euo pipefail

tag="${1:?usage: script/build.sh <tag>}"
mkdir -p dist
for platform in darwin-amd64 darwin-arm64 linux-amd64 linux-arm64; do
  GOOS="${platform%-*}" GOARCH="${platform#*-}" CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w" -o "dist/gh-csd_${tag}_${platform}" .
done
