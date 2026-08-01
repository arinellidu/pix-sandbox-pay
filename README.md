# pix-sandbox

> A drop-in Pix emulator — the full instant-payment lifecycle (charge → EMV QR → payment → refund → webhook) in a single binary. Built for teams integrating with Brazilian PSPs, and for FedNow/SEPA teams studying the world's largest instant-payments deployment.

**Status:** S0 — executable skeleton. Health check, mock OAuth2, embedded SQLite event log. Charges, EMV and payments land in S1/S2; see [docs/DESIGN.md](docs/DESIGN.md).

## Run it

```bash
docker build -t pix-sandbox . && docker run -p 8080:8080 pix-sandbox
```

Or straight from source (Go 1.26, no CGO, no external services):

```bash
go run ./cmd/pix-sandbox
```

`make run` (Linux/macOS) and `.\run.ps1` (Windows) are wrappers over exactly that.

```bash
curl :8080/health
# {"status":"ok"}

curl -X POST :8080/oauth/token -d 'grant_type=client_credentials&client_id=demo'
# {"access_token":"sandbox_...","token_type":"Bearer","expires_in":3600,"scope":"..."}
```

## Endpoints (S0)

| Method & path | Purpose |
|---|---|
| `GET /health` | Liveness; also pings the store |
| `POST /oauth/token` | Mock client-credentials grant — accepts a form or JSON body, and no body at all |

The rest of the [API Pix surface](docs/DESIGN.md#6-api-surface-v1) arrives phase by phase.

## Configuration

| Flag | Env | Default | Meaning |
|---|---|---|---|
| `-addr` | `PIX_SANDBOX_ADDR` | `:8080` | Listen address |
| `-db` | `PIX_SANDBOX_DB` | `./data/sandbox.db` | SQLite file; created at boot with its parent directory |
| `-seed` | `PIX_SANDBOX_SEED` | fixed | Seed for the single random source |

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

Layout: `cmd/pix-sandbox` (entrypoint) · `internal/api` (HTTP) · `internal/store` (append-only event log + projections) · `internal/rng` (seeded source).

The `events` table is append-only — SQLite triggers reject `UPDATE` and `DELETE` — because the log is the source of truth and projections are derived from it (INV-3).

First consumer: [arinelli-pay](https://github.com/arinelliquebec/arinelli-pay), a multi-rail billing SaaS that uses this emulator as its local Pix provider.
