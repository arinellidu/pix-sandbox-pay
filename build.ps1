<#
.SYNOPSIS
    Build the static, CGO-free binary into .\bin. Equivalent to `make build`.

.EXAMPLE
    .\build.ps1
    .\build.ps1 -Version v0.1.0

.NOTES
    Same thing as:
      $env:CGO_ENABLED = '0'
      go build -trimpath -ldflags "-s -w" -o bin/pix-sandbox.exe ./cmd/pix-sandbox
    With no -Version, the binary resolves its build from the VCS stamp.
#>
param(
    [string]$Version = ''
)

$ErrorActionPreference = 'Stop'
& (Join-Path $PSScriptRoot 'make.ps1') build -Version $Version
exit $LASTEXITCODE
