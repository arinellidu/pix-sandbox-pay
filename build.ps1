<#
.SYNOPSIS
    Build the static, CGO-free binary into .\bin. Equivalent to `make build`.

.EXAMPLE
    .\build.ps1
    .\build.ps1 -Version v0.1.0

.NOTES
    Same thing as:
      $env:CGO_ENABLED = '0'
      go build -trimpath -ldflags "-s -w -X main.version=dev" -o bin/pix-sandbox.exe ./cmd/pix-sandbox
#>
param(
    [string]$Version = 'dev'
)

$ErrorActionPreference = 'Stop'
& (Join-Path $PSScriptRoot 'make.ps1') build -Version $Version
exit $LASTEXITCODE
