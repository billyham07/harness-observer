# Build the embedded dashboard.
#
# Node and npm are build-time only. This writes internal/web/assets/dist, which
# go:embed picks up on the next `go build`; the shipped binary needs no Node
# runtime, no dev server and no network.
$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
Push-Location (Join-Path $root 'webui')
try {
    if (Test-Path 'package-lock.json') {
        npm ci
    } else {
        npm install
    }
    if ($LASTEXITCODE -ne 0) { throw "npm install failed with exit code $LASTEXITCODE" }

    npm run build
    if ($LASTEXITCODE -ne 0) { throw "npm run build failed with exit code $LASTEXITCODE" }
}
finally {
    Pop-Location
}

Write-Output "embedded assets: $(Join-Path $root 'internal\web\assets\dist')"
