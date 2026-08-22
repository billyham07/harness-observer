<#
.SYNOPSIS
Fetch the prebuilt DuckDB client library this repo links against.

.DESCRIPTION
harness-observer links DuckDB dynamically (-tags=duckdb_use_lib). The static
libraries shipped in duckdb-go-bindings are built against a libstdc++ that uses
emulated TLS, which current MSYS2 GCC (16.x, native TLS) cannot link: it fails
on __emutls_v._ZSt11__once_call. The official release library has no such
problem because only the C API crosses the boundary.

Build with the MSYS2 UCRT64 toolchain (C:\msys64\ucrt64\bin), not MINGW64:
the msvcrt-flavoured mingw64 gcc compiles go-duckdb's C++ glue into a process
whose heap corrupts on the first driver call (exit 0xc0000374), even though a
plain C test against the same DLL passes. UCRT64-built binaries run clean.

.EXAMPLE
scripts\fetch-duckdb.ps1
scripts\fetch-duckdb.ps1 -Version 1.4.1
#>
param([string]$Version = "1.4.1")

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$dest = Join-Path $root "third_party\duckdb"
$asset = "libduckdb-windows-amd64.zip"
$url = "https://github.com/duckdb/duckdb/releases/download/v$Version/$asset"

New-Item -ItemType Directory -Force $dest | Out-Null
$zip = Join-Path $dest $asset
Write-Host "fetching $url"
Invoke-WebRequest -Uri $url -OutFile $zip
Expand-Archive -Path $zip -DestinationPath $dest -Force
Remove-Item $zip

Write-Host "installed DuckDB $Version into $dest"
Write-Host ""
Write-Host "build and test with (UCRT64 gcc, not MINGW64 - see description):"
Write-Host "  `$env:CC = `"C:\msys64\ucrt64\bin\gcc.exe`"; `$env:CXX = `"C:\msys64\ucrt64\bin\g++.exe`""
Write-Host "  `$env:PATH = `"C:\msys64\ucrt64\bin;$dest;`$env:PATH`""
Write-Host "  `$env:CGO_LDFLAGS = `"-L$dest -lduckdb`""
Write-Host "  go test -tags=duckdb_use_lib ./..."
