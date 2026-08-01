<#
.SYNOPSIS
    Windows equivalent of the Makefile. Same targets, same behaviour.

.EXAMPLE
    .\make.ps1 run
    .\make.ps1 run -addr :9090 -db .\tmp\sandbox.db
    .\make.ps1 build -Version v0.1.0
    .\make.ps1 test

.NOTES
    Nothing here is required to work on the project: every target is a thin
    wrapper over a plain `go` command that runs fine on its own.

    Deliberately NOT an advanced script: no [CmdletBinding()] and no
    [Parameter()] attribute, either of which switches on strict binding. That
    matters twice over — strict binding rejects unknown flags outright instead
    of collecting them in $args, and the common parameters it adds would let
    `-db` bind to `-Debug` by prefix and vanish. Validation attributes are
    fine; they do not make a script advanced.
#>
param(
    [ValidateSet('run', 'test', 'test-race', 'build', 'docker-build', 'docker-run',
                 'tidy', 'fmt', 'vet', 'lint', 'clean', 'help')]
    [string]$Target = 'help',

    [string]$Version = 'dev',
    [string]$Image = 'ghcr.io/arinelliquebec/pix-sandbox'
)

$ErrorActionPreference = 'Stop'
Set-Location -Path $PSScriptRoot

# Anything not bound above is forwarded to the underlying go command, e.g.
#   .\make.ps1 run -addr :9090
# Captured here because $args inside a script block refers to that block.
$Extra = @($args)

$Package = './cmd/pix-sandbox'
$Binary = 'bin/pix-sandbox.exe'
$LdFlags = "-s -w -X main.version=$Version"

# Native executables do not throw; check the exit code explicitly.
function Invoke-Step {
    param([scriptblock]$Step)
    & $Step
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

# Scope CGO_ENABLED to one command instead of leaking it into the caller's shell.
function Invoke-WithCgo {
    param([string]$Enabled, [scriptblock]$Step)
    $previous = $env:CGO_ENABLED
    $env:CGO_ENABLED = $Enabled
    try { Invoke-Step $Step }
    finally {
        if ($null -eq $previous) { Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue }
        else { $env:CGO_ENABLED = $previous }
    }
}

function Show-Help {
    Write-Output @'
Targets (mirror of the Makefile):

  run           start the sandbox on :8080          go run ./cmd/pix-sandbox
  test          run the test suite                  go test ./...
  test-race     same, under the race detector       needs CGO and a C toolchain
  build         static CGO-free binary in .\bin
  docker-build  build the distroless image
  docker-run    run the image on :8080
  tidy          go mod tidy
  fmt           go fmt ./...
  vet           go vet ./...
  lint          fmt + vet
  clean         remove .\bin and .\data

Options: -Version <v> (build/docker-build)  -Image <ref> (docker targets)

Unrecognized *named* arguments are forwarded to the go command, e.g.
  .\make.ps1 run -addr :9090 -seed 42
Bare positional extras are not: they bind to -Version and -Image instead.
Every flag the sandbox and `go test` take is named anyway.
'@
}

switch ($Target) {
    'run' {
        Invoke-Step { go run $Package @Extra }
    }
    'test' {
        Invoke-Step { go test ./... @Extra }
    }
    'test-race' {
        if (-not (Get-Command gcc -ErrorAction SilentlyContinue)) {
            Write-Warning 'No C toolchain on PATH; -race needs one. Install mingw-w64 or run this target in CI.'
        }
        Invoke-WithCgo '1' { go test -race ./... @Extra }
    }
    'build' {
        Invoke-WithCgo '0' { go build -trimpath -ldflags $LdFlags -o $Binary $Package }
        $mb = [math]::Round((Get-Item $Binary).Length / 1MB, 1)
        Write-Output "built $Binary ($mb MB, version $Version)"
    }
    'docker-build' {
        Invoke-Step { docker build --build-arg "VERSION=$Version" -t "${Image}:$Version" . }
    }
    'docker-run' {
        Invoke-Step { docker run --rm -p 8080:8080 "${Image}:$Version" }
    }
    'tidy' { Invoke-Step { go mod tidy } }
    'fmt'  { Invoke-Step { go fmt ./... } }
    'vet'  { Invoke-Step { go vet ./... } }
    'lint' {
        Invoke-Step { go fmt ./... }
        Invoke-Step { go vet ./... }
    }
    'clean' {
        foreach ($dir in 'bin', 'data') {
            if (Test-Path $dir) {
                Remove-Item -Recurse -Force $dir
                Write-Output "removed $dir"
            }
        }
    }
    default { Show-Help }
}
