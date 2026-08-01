<#
.SYNOPSIS
    Start the sandbox on :8080. Equivalent to `make run`.

.EXAMPLE
    .\run.ps1
    .\run.ps1 -addr :9090 -seed 42

.NOTES
    Same thing as running `go run ./cmd/pix-sandbox` yourself; every flag is
    forwarded untouched. No param block on purpose — a declared parameter (or
    the common ones an advanced function brings) would capture flags meant for
    the binary, `-db` being the obvious casualty.
#>
$ErrorActionPreference = 'Stop'
Set-Location -Path $PSScriptRoot

go run ./cmd/pix-sandbox @args
exit $LASTEXITCODE
