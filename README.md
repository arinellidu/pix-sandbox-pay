# pix-sandbox

> A drop-in Pix emulator — the full instant-payment lifecycle (charge → EMV QR → payment → refund → webhook) in a single binary. Built for teams integrating with Brazilian PSPs, and for FedNow/SEPA teams studying the world's largest instant-payments deployment.

**Status:** S2 — the loop closes. Create a charge, pay it, refund it, and your endpoint gets the signed callback. The embedded console and the v0.1.0 release land in S3; see [docs/DESIGN.md](docs/DESIGN.md).

## Run it

```bash
docker build -t pix-sandbox . && docker run -p 8080:8080 pix-sandbox
```

Or straight from source (Go 1.26, no CGO, no external services):

```bash
go run ./cmd/pix-sandbox
```

`make run` (Linux/macOS) and `.\run.ps1` (Windows) are wrappers over exactly that.

## The 60-second loop

```bash
curl :8080/health
# {"status":"ok"}

# Where the callbacks should go. Any local echo server will do.
curl -X PUT :8080/webhook/dev@example.com -H 'Content-Type: application/json' \
     -d '{"webhookUrl":"http://127.0.0.1:9099/pix"}'

curl -X POST :8080/cob -H 'Content-Type: application/json' \
     -d '{"valor":{"original":"10.00"},"chave":"dev@example.com"}'
# 201 → the charge, with txid, status ATIVA and a payable pixCopiaECola

curl -X POST :8080/sandbox/pay -H 'Content-Type: application/json' \
     -d '{"txid":"<txid>","infoPagador":"Coffee"}'
# 201 → the pix, with an e2eId — and a signed POST lands on your endpoint:
#       {"pix":[{"endToEndId":"E12345678...","txid":"...","valor":"10.00", ...}]}

curl -X PUT :8080/pix/<e2eId>/devolucao/dev1 -H 'Content-Type: application/json' \
     -d '{"valor":"10.00"}'
# 201 → the devolução, DEVOLVIDO, and a second callback carrying it
```

## Endpoints

| Method & path | Purpose |
|---|---|
| `GET /health` | Liveness; also pings the store |
| `POST /oauth/token` | Mock client-credentials grant — accepts a form or JSON body, and no body at all |
| `POST /cob` | Create an immediate charge; mints a txid when the body omits one |
| `PUT /cob/{txid}` | Create with a txid you choose |
| `GET /cob/{txid}` | Read the charge; settles a pending expiry first |
| `GET /cob/{txid}/qrcode` | `{"qrcode": "<EMV payload>", "imagemQrcode": null}` |
| `GET /pix/{e2eId}` | Read a received payment and its refunds |
| `PUT /pix/{e2eId}/devolucao/{id}` | Refund a payment; idempotent on the id you choose |
| `PUT /webhook/{chave}` · `GET /webhook/{chave}` | Register and inspect the callback endpoint for a key |
| **Sandbox-only:** `POST /sandbox/pay` | Simulate a payer settling a charge |

The rest of the [API Pix surface](docs/DESIGN.md#6-api-surface-v1) arrives phase by phase.

Errors are RFC 7807 documents (`application/problem+json`) shaped like BACEN's, listing every violation at once:

```json
{
  "type": "https://pix.bcb.gov.br/api/v2/error/CobOperacaoInvalida",
  "title": "Cobrança inválida.", "status": 400,
  "violacoes": [{"razao": "amount \"10\" must have two decimal places", "propriedade": "valor.original"}]
}
```

## Charges and BR Codes

- **Idempotent on txid** (INV-2). Replaying `POST /cob` or `PUT /cob/{txid}` with a txid that already exists returns the stored charge untouched, with **200** instead of 201 so a replay is visible on the wire.
- **The BR Code is self-contained**: the Pix key sits in field 26-01 and the txid in 62-05, so a payload resolves without fetching a location document. `loc.location` is reported for shape compatibility but nothing needs to dereference it. The strict dynamic flavour (URL in 26-25, `***` in 62-05) arrives with the location endpoint.
- **CRC16-CCITT-FALSE** closes field 63. The test suite pins it against the canonical `123456789` → `0x29B1` check value and against the example printed in BACEN's BR Code manual.
- **Expiry is a recorded transition, not an inference.** Reading a charge past its window moves it to `EXPIRADA` and appends `cob.expired` to the log (INV-3). `EXPIRADA` is the sandbox's own state: BACEN leaves expiry for the client to derive from `calendario`, while the design models it explicitly.
- Money is `int64` cents everywhere inside; the `"10.00"` string exists only at the edge.

## Payments, refunds and callbacks

- **`POST /sandbox/pay` is the payer's half of the loop**, which no BACEN endpoint covers because in production it happens at another PSP — exactly the part a local emulator has to stand in for. It settles the charge, mints the `e2eId` and notifies the payee.
- **Identifiers are unique by construction** (INV-2). An `e2eId` is `E` + ISPB + a minute-precision timestamp + six seeded characters + a base-36 sequence; the sequence is drawn inside the transaction that stores the row, so two identifiers minted in the same minute cannot collide however the random half falls. `rtrId` is the same shape with a `D`.
- **A charge settles once.** Paying it again is a `409` naming the payment that already exists — and the `payments` table holds the txid unique, so the database refuses a double settlement too.
- **Refunds are bounded by what settled** (INV-4), in the handler and again as a `CHECK` constraint. Only full refunds land in this phase; `PUT /pix/{e2eId}/devolucao/{id}` is idempotent on the id you choose, replying `200` instead of `201` on a replay.
- **Callbacks are signed, retried and logged.** The body is `{"pix":[...]}`, `X-Signature` carries the hex HMAC-SHA256 of the exact bytes sent, and a failure is retried after 1s, 5s and 25s. Delivery is asynchronous — the API response never waits on the payee's endpoint — and both outcomes reach the event log on that payment's aggregate, so `webhook.delivered` and `webhook.failed` are part of the story the timeline tells. A `4xx` other than `429` is not retried: the receiver has said the request itself is wrong.
- The dispatcher posts to the URL exactly as registered. Real PSPs append `/pix` to it; the sandbox does not, because the demo loop is easier to read when the URL you registered is the URL that gets called.

## Configuration

| Flag | Env | Default | Meaning |
|---|---|---|---|
| `-addr` | `PIX_SANDBOX_ADDR` | `:8080` | Listen address |
| `-db` | `PIX_SANDBOX_DB` | `./data/sandbox.db` | SQLite file; created at boot with its parent directory |
| `-seed` | `PIX_SANDBOX_SEED` | fixed | Seed for the single random source |
| `-base-url` | `PIX_SANDBOX_BASE_URL` | `localhost:8080` | Scheme-less host used to build `loc.location` |
| `-merchant-name` | `PIX_SANDBOX_MERCHANT_NAME` | `PIX SANDBOX` | BR Code field 59 |
| `-merchant-city` | `PIX_SANDBOX_MERCHANT_CITY` | `SAO PAULO` | BR Code field 60 |
| — | `WEBHOOK_SECRET` | `pix-sandbox` | Key the `X-Signature` HMAC is computed with |

`WEBHOOK_SECRET` has no flag on purpose: a secret does not belong on a command line, where every process on the box can read it. Its default is a published constant so the loop verifies out of the box — override it when the receiver under test checks signatures against its own.

The sandbox is **deterministic by default**: every generated value comes from one seeded source, and the seed is printed at boot — rerun with the same seed to reproduce a run exactly.

## Development

Plain Go commands are the source of truth; the task runners only wrap them.

```bash
go test ./...                                          # test
go build -trimpath -o bin/pix-sandbox ./cmd/pix-sandbox # build
go vet ./...                                           # vet
```

| Task | Linux/macOS | Windows |
|---|---|---|
| Run on :8080 | `make run` | `.\run.ps1` |
| Test | `make test` | `.\make.ps1 test` |
| Build static binary | `make build` | `.\build.ps1` |
| Lint (fmt + vet) | `make lint` | `.\make.ps1 lint` |
| Build image | `make docker-build` | `.\make.ps1 docker-build` |
| All targets | `make help` | `.\make.ps1` |

`make.ps1` mirrors the Makefile target for target; `run.ps1` and `build.ps1` are shortcuts for the two you reach for most. Named flags pass straight through — `.\run.ps1 -addr :9090 -seed 42`. If PowerShell blocks the scripts, run once with `powershell -ExecutionPolicy Bypass -File .\run.ps1`.

Layout: `cmd/pix-sandbox` (entrypoint) · `internal/api` (HTTP) · `internal/core` (domain, no I/O) · `internal/emv` (BR Code TLV + CRC) · `internal/store` (append-only event log + projections) · `internal/webhook` (signed delivery with retries) · `internal/rng` (seeded source).

The `events` table is append-only — SQLite triggers reject `UPDATE` and `DELETE` — because the log is the source of truth and projections are derived from it (INV-3). A state change and its event share one transaction, so neither can exist without the other.

Schema changes are numbered files under `internal/store/migrations/`, applied on boot and tracked in SQLite's `user_version`. A database from a newer binary is refused rather than downgraded.

First consumer: [arinelli-pay](https://github.com/arinelliquebec/arinelli-pay), a multi-rail billing SaaS that uses this emulator as its local Pix provider.
