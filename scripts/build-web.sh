#!/usr/bin/env sh
# Build the embedded dashboard.
#
# Node and npm are build-time only. This writes internal/web/assets/dist, which
# go:embed picks up on the next `go build`; the shipped binary needs no Node
# runtime, no dev server and no network.
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root/webui"

if [ -f package-lock.json ]; then
  npm ci
else
  npm install
fi
npm run build

echo "embedded assets: $root/internal/web/assets/dist"
