# Changelog

All notable changes to this project are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.2] — 2026-08-01

### Fixed
- The release job now creates the directory it cross-compiles into. `go build -o build/...` does not create its output's parent, and the loop removed that directory every iteration, so v0.1.1 published its image but no binaries. Archives are `.tar.gz` on every target, Windows included — the zip path had never been executed on the machine that checked it.

## [0.1.1] — 2026-08-01

### Added
- Apache-2.0 licence, formalised in `LICENSE` — the adoption path should never be blocked by an undecided licence.
- Release binaries: a tag now cross-compiles for linux, macOS and Windows on amd64 and arm64, publishes the `.tar.gz` archives with `checksums.txt` on the GitHub release, and keeps shipping the multi-arch image. The binary is static and CGO-free, so Docker is a convenience rather than a requirement.

## [0.1.0] — 2026-08-01

First release: the Pix lifecycle runs end to end in one binary. `docker run`, create a charge, pay it, refund it, and your endpoint gets the signed callback — with every transition readable in the embedded console.

### Added
- S3 console and release plumbing: an embedded read-only UI at `/console` (templ + htmx, assets fingerprinted and served from the binary — no external request) showing the charge ledger and the recorded timeline of any txid; GitHub Actions running vet, gofmt, a generated-code drift check, `-race` and the demo loop end to end, plus a tagged release that pushes a multi-arch image to ghcr.io; a `vhs` tape for the demo GIF; and `DESIGN.md` recording the console's visual system.
- S2 payments: `POST /sandbox/pay`, `GET /pix/{e2eId}`, `PUT /pix/{e2eId}/devolucao/{id}` and `PUT`/`GET /webhook/{chave}`, with e2eId/rtrId unique by construction, full refunds bounded by what settled (INV-4, enforced in SQL too), and an asynchronous webhook dispatcher that signs with HMAC-SHA256, retries at 1s/5s/25s and records every outcome in the event log.
- S1 charges: `POST /cob`, `PUT /cob/{txid}`, `GET /cob/{txid}` and `GET /cob/{txid}/qrcode`, idempotent on txid, with a self-contained EMV BR Code (key in 26-01, txid in 62-05, CRC16-CCITT-FALSE in 63), lazily recorded expiry, BACEN-shaped RFC 7807 errors, and versioned SQLite migrations.
- S0 skeleton: chi router with `GET /health` and a mock `POST /oauth/token`, embedded CGO-free SQLite store with the append-only `events` log and the `charges` projection, seeded deterministic random source, Makefile, and a multi-stage distroless Dockerfile.
- PowerShell task runners for Windows (`make.ps1`, `run.ps1`, `build.ps1`) and a `help` target in the Makefile.
