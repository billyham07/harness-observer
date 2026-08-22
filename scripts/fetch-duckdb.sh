#!/usr/bin/env bash
# Fetch the prebuilt DuckDB client library this repo links against.
#
# harness-observer links DuckDB dynamically (-tags=duckdb_use_lib). The static
# libraries shipped in duckdb-go-bindings are built against a libstdc++ that
# uses emulated TLS, which current MSYS2 GCC (16.x, native TLS) cannot link:
# it fails on __emutls_v._ZSt11__once_call. The official release library has no
# such problem because only the C API crosses the boundary.
#
# Usage: scripts/fetch-duckdb.sh [version]
set -euo pipefail

VERSION="${1:-1.4.1}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="$ROOT/third_party/duckdb"

case "$(uname -s)" in
  Linux*)   ASSET="libduckdb-linux-amd64.zip" ;;
  Darwin*)  ASSET="libduckdb-osx-universal.zip" ;;
  MINGW*|MSYS*|CYGWIN*) ASSET="libduckdb-windows-amd64.zip" ;;
  *) echo "unsupported platform: $(uname -s)" >&2; exit 1 ;;
esac
if [ "$(uname -m)" = "aarch64" ] && [ "$ASSET" = "libduckdb-linux-amd64.zip" ]; then
  ASSET="libduckdb-linux-arm64.zip"
fi

URL="https://github.com/duckdb/duckdb/releases/download/v${VERSION}/${ASSET}"
mkdir -p "$DEST"
echo "fetching $URL"
curl -sSfL -o "$DEST/$ASSET" "$URL"
# unzip is not present in every environment this runs in (Git Bash on Windows,
# minimal CI images); python is, and it ships zipfile.
if command -v unzip >/dev/null 2>&1; then
  ( cd "$DEST" && unzip -oq "$ASSET" && rm -f "$ASSET" )
else
  python3 -c "import sys, zipfile; zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])" "$DEST/$ASSET" "$DEST"     || python -c "import sys, zipfile; zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])" "$DEST/$ASSET" "$DEST"
  rm -f "$DEST/$ASSET"
fi

echo "installed DuckDB $VERSION into $DEST"
echo
echo "build and test with:"
echo "  export CGO_LDFLAGS=\"-L$DEST -lduckdb\""
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) echo "  export PATH=\"$DEST:\$PATH\"            # duckdb.dll must be findable at runtime" ;;
  Darwin*)              echo "  export DYLD_LIBRARY_PATH=\"$DEST\"" ;;
  *)                    echo "  export LD_LIBRARY_PATH=\"$DEST\"" ;;
esac
echo "  go test -tags=duckdb_use_lib ./..."
