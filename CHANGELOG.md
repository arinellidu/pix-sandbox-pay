# Changelog

All notable changes to this project are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- S2 payments: `POST /sandbox/pay`, `GET /pix/{e2eId}`, `PUT /pix/{e2eId}/devolucao/{id}` and `PUT`/`GET /webhook/{chave}`, with e2eId/rtrId unique by construction, full refunds bounded by what settled (INV-4, enforced in SQL too), and an asynchronous webhook dispatcher that signs with HMAC-SHA256, retries at 1s/5s/25s and records every outcome in the event log.
- S1 charges: `POST /cob`, `PUT /cob/{txid}`, `GET /cob/{txid}` and `GET /cob/{txid}/qrcode`, idempotent on txid, with a self-contained EMV BR Code (key in 26-01, txid in 62-05, CRC16-CCITT-FALSE in 63), lazily recorded expiry, BACEN-shaped RFC 7807 errors, and versioned SQLite migrations.
- S0 skeleton: chi router with `GET /health` and a mock `POST /oauth/token`, embedded CGO-free SQLite store with the append-only `events` log and the `charges` projection, seeded deterministic random source, Makefile, and a multi-stage distroless Dockerfile.
- PowerShell task runners for Windows (`make.ps1`, `run.ps1`, `build.ps1`) and a `help` target in the Makefile.
