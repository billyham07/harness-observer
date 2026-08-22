#!/usr/bin/env bash
# Build one release archive for the platform this runs on.
#
# The same script produces the artifact locally and in CI, so a release is
# something you can reproduce on your own machine rather than something only
# the workflow knows how to make.
#
# Usage: scripts/package-release.sh <version>
set -euo pipefail

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  echo "usage: scripts/package-release.sh <version>   (e.g. v0.1.0)" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DUCKDB_VERSION="${DUCKDB_VERSION:-1.4.1}"

# DuckDB is linked dynamically, so the client library ships next to the binary.
# Which file that is, and what the archive is called, is all this switch does.
case "$(uname -s)" in
  Linux*)               TARGET="linux-amd64";   BINARY="harness-observer";     RUNTIME="libduckdb.so" ;;
  Darwin*)              TARGET="darwin-arm64";  BINARY="harness-observer";     RUNTIME="libduckdb.dylib" ;;
  MINGW*|MSYS*|CYGWIN*) TARGET="windows-amd64"; BINARY="harness-observer.exe"; RUNTIME="duckdb.dll" ;;
  *) echo "unsupported platform: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  aarch64) [ "$TARGET" = "linux-amd64" ] && TARGET="linux-arm64" ;;
  x86_64)  [ "$TARGET" = "darwin-arm64" ] && TARGET="darwin-amd64" ;;
esac

NAME="harness-observer_${VERSION}_${TARGET}"
STAGE="$ROOT/dist/stage/$NAME"

if [ ! -f "$ROOT/third_party/duckdb/$RUNTIME" ]; then
  "$ROOT/scripts/fetch-duckdb.sh" "$DUCKDB_VERSION"
fi

# Node and npm are build-time only, and this is the build time.
"$ROOT/scripts/build-web.sh"

rm -rf "$ROOT/dist/stage"
mkdir -p "$STAGE"

export CGO_ENABLED=1
export CGO_LDFLAGS="-L$ROOT/third_party/duckdb -lduckdb"

# -s -w drop the symbol and DWARF tables; the version is the only thing stamped
# in, and it is what `harness-observer version` reports back.
LDFLAGS="-s -w -X github.com/billyham07/harness-observer/internal/buildinfo.version=$VERSION"

# The DuckDB library ships beside the binary, so the binary has to look beside
# itself for it. Windows already searches the executable's own directory; ELF
# and Mach-O do not, and an unzipped archive fails on its first run with
# "error while loading shared libraries: libduckdb.so".
case "$(uname -s)" in
  Linux*)  LDFLAGS="$LDFLAGS -extldflags '-Wl,-rpath,\$ORIGIN'" ;;
  Darwin*) LDFLAGS="$LDFLAGS -extldflags '-Wl,-rpath,@loader_path'" ;;
esac

( cd "$ROOT" && go build \
    -tags=duckdb_use_lib \
    -ldflags "$LDFLAGS" \
    -o "$STAGE/$BINARY" \
    ./cmd/harness-observer )

cp "$ROOT/third_party/duckdb/$RUNTIME" "$STAGE/$RUNTIME"

sed -e "s/{{VERSION}}/$VERSION/g" \
    -e "s/{{BINARY}}/$BINARY/g" \
    -e "s/{{RUNTIME}}/$RUNTIME/g" \
    -e "s/{{DUCKDB}}/$DUCKDB_VERSION/g" \
    "$ROOT/scripts/release-readme.txt" > "$STAGE/README.txt"

# unzip/zip are not present in every environment this runs in (Git Bash on
# Windows, minimal CI images); python is, and it ships zipfile.
#
# The candidate has to be run, not just found: on Windows, `python3` resolves to
# the Microsoft Store alias stub, which is on PATH, is not Python, and exits 49
# without saying why.
PYTHON=""
for candidate in python3 python py; do
  if command -v "$candidate" >/dev/null 2>&1 && "$candidate" -c "import zipfile" >/dev/null 2>&1; then
    PYTHON="$candidate"
    break
  fi
done
if [ -z "$PYTHON" ]; then
  echo "no working python found; one is needed to build the archive" >&2
  exit 1
fi
"$PYTHON" -c "import shutil, sys; shutil.make_archive(sys.argv[1], 'zip', sys.argv[2], sys.argv[3])" \
  "$ROOT/dist/$NAME" "$ROOT/dist/stage" "$NAME"
rm -rf "$ROOT/dist/stage"

echo "packaged $ROOT/dist/$NAME.zip"
