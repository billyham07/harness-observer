# Building

Two prerequisites, both fetched or generated rather than committed: the DuckDB
client library, and the dashboard bundle. The store is DuckDB, which means cgo
and a C toolchain; that cost is deliberate. What follows is the shortest working
path on each platform.

```bash
scripts/fetch-duckdb.sh          # or: scripts\fetch-duckdb.ps1 on PowerShell
scripts/build-web.sh             # or: scripts\build-web.ps1
export CGO_LDFLAGS="-L$PWD/third_party/duckdb -lduckdb"
export GOFLAGS=-tags=duckdb_use_lib
export LD_LIBRARY_PATH="$PWD/third_party/duckdb"    # Windows: add the dir to PATH
go test ./...
go build -o bin/harness-observer ./cmd/harness-observer
```

`third_party/` is gitignored: it is ~36 MB of prebuilt binary that every
checkout can fetch itself.

## The dashboard bundle

`internal/web/assets/dist` is generated from [`webui/`](../webui) by Vite and
embedded by `go:embed`. It is gitignored for the same reason `third_party/` is:
it is build output, and a committed copy is a copy that can disagree with its
source.

```bash
cd webui
npm ci
npm run typecheck
npm test -- --run
npm run build            # writes ../internal/web/assets/dist
```

Node and npm are **build-time only**. Nothing about the shipped executable needs
them: the bundle is inside the binary, and the dashboard runs offline with no
Node runtime, no dev server and no CDN. A binary built without this step still
serves the API and answers `/` with a page naming the step that was skipped, so
the failure is never a mystery.

For frontend work, `npm run dev` proxies `/api` to a running
`harness-observer web` on `127.0.0.1:9464`, which avoids relinking Go on every
change.

## Why dynamic linking

`go-duckdb` ships prebuilt static libraries and links them by default. On
Windows those libraries do not link with a current MSYS2 toolchain:

```text
libicu_extension.a(ub_duckdb_icu_common.cpp.obj):
  undefined reference to `__emutls_v._ZSt11__once_call'
  undefined reference to `__emutls_v._ZSt15__once_callable'
```

They were built against a libstdc++ that uses GCC's emulated TLS; MSYS2's
current `mingw-w64-ucrt-x86_64-gcc` (16.2.0) uses native TLS and does not define
those control objects. Reproduced with go-duckdb v2.4.3 (bindings v0.1.21) and
v2.0.0 (bindings v0.1.4), under both `mingw64` and `ucrt64`, so it is the
toolchain and not a DuckDB version. The `ucrt64` toolchain is exactly what
go-duckdb's own Windows instructions ask for.

Dynamic linking against the official release library sidesteps it entirely:
only DuckDB's C API crosses the boundary, so the C++ ABI of the library and the
ABI of the Go build never have to agree. The cost is that `duckdb.dll` (or
`libduckdb.so`) must sit next to the binary or on the library path.

Anyone whose toolchain *can* link the static libraries can drop
`-tags=duckdb_use_lib` and `CGO_LDFLAGS` and build a single self-contained
binary; nothing in this repo depends on the linking mode.

The native `gpui` window is Windows-only WebView2 via `github.com/jchv/go-webview2`.
It does not add CGO. The Evergreen WebView2 runtime is already on current Windows
10/11; if it is missing, `gpui` prints a warning and opens a browser instead.

## Toolchain

| Platform | Compiler |
| --- | --- |
| Windows | MSYS2 **`ucrt64`** GCC on PATH (see below), or any other cgo-capable UCRT toolchain |
| Linux | the distribution's GCC |
| macOS | Xcode command line tools |

### Windows: ucrt64, not mingw64

The official `duckdb.dll` is built against the UCRT. Building against it with the
MSYS2 `mingw64` toolchain (msvcrt) links cleanly and then corrupts the heap at
runtime: every process that opens a database dies with `0xC0000374`, including
`go test ./internal/store`, `doctor` and `web`. Reproduced with GCC 16.2.0 and
DuckDB 1.4.1. The two runtimes have separate heaps, and the C API hands
allocations across that boundary.

Put the `ucrt64` toolchain first on PATH, and set `CC` explicitly if a `mingw64`
gcc is also installed:

```powershell
$env:PATH = "C:\msys64\ucrt64\bin;$env:PATH"
$env:CC = "C:\msys64\ucrt64\bin\gcc.exe"
```

## Runtime

The library has to be findable when the binary runs, not just when it links:

```powershell
$env:PATH = "$PWD\third_party\duckdb;$env:PATH"   # Windows
```

```bash
export LD_LIBRARY_PATH="$PWD/third_party/duckdb"  # Linux
export DYLD_LIBRARY_PATH="$PWD/third_party/duckdb" # macOS
```

A missing library shows up as exit status `0xc0000135` on Windows and
`error while loading shared libraries` on Linux.

## One writer per database

DuckDB takes an exclusive lock on the database file for the whole process. Two
processes cannot both have it open read-write, so `harness-observer web`/`gpui`
and a separate `harness-observer ingest` will not run against the same store at
the same time — the second one fails to open it. This differs from the previous
SQLite/WAL store, which allowed it.

In practice: `web` and `gpui` own the store while they run, and they ingest on
their own timer, so a second scheduled ingest is redundant anyway. Use `--db`
to point a second process at its own file if you really need both.

## Storage layout

The store lives at `harness-observer.duckdb` in the per-user data directory
(`%LOCALAPPDATA%\harness-observer\` on Windows), overridable with `--db` or
`$HARNESS_OBSERVER_DB`.

An older `harness-observer.db` from the SQLite releases is left exactly where it
is: never opened, never migrated, never deleted. Normalized data is rebuilt from
the session evidence instead, which is both safer than a row-level migration and
the only approach that stays correct when normalization itself has changed.
`ingest` and `doctor` mention the file once so it is not an unexplained orphan;
deleting it is your call.

## Releasing

Releases are cut by pushing a tag; the `release` workflow builds one archive
per platform and publishes them.

```bash
git tag v0.1.0
git push origin v0.1.0
```

Tag a commit that is already green on `main`: the tag push runs the test
workflow too, but it runs alongside the release rather than gating it.

Each archive carries the binary, the DuckDB client library it links against,
and a README naming both. The version in the tag is stamped into the binary
with `-ldflags`, and the workflow unpacks what it is about to publish and asks
it:

```text
harness-observer v0.1.0
go1.25.0 windows/amd64
store schema 3 · parser 5
```

The same archive can be built locally, which is the point of doing it in a
script rather than in the workflow:

```bash
scripts/package-release.sh v0.1.0     # writes dist/harness-observer_v0.1.0_<target>.zip
```

It fetches DuckDB if needed, builds the frontend, links the binary and zips the
result for whatever platform it runs on.

On Linux and macOS the release binary is linked with an rpath of `$ORIGIN` /
`@loader_path`, so it finds the DuckDB library shipped beside it. Windows
already searches the executable's own directory; ELF and Mach-O do not, and
without it an unzipped archive fails on its first run with *error while loading
shared libraries*.

An untagged build still answers `harness-observer version`: `go install` leaves
a module version, and a plain `go build` records the commit, so the output is
`dev+<commit>` rather than nothing. A build from a dirty tree says `.dirty`.
