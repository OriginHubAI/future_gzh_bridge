#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${ROOT}/dist/wcplus-bridge"
mkdir -p "$OUT/configs" "$OUT/scripts"
export GOOS=windows GOARCH=amd64
go build -o "$OUT/bridge.exe" "$ROOT/cmd/bridge"
go build -o "$OUT/wcplus-mcp.exe" "$ROOT/cmd/mcp-server"
cp "$ROOT/configs/config.windows.yaml" "$OUT/configs/config.yaml"
rm -rf "$OUT/scripts/windows-simulator"
mkdir -p "$OUT/scripts/windows-simulator"
cp -R "$ROOT/scripts/windows-simulator/." "$OUT/scripts/windows-simulator/"
echo "Built: $OUT/bridge.exe $OUT/wcplus-mcp.exe (with scripts/windows-simulator helper)"
