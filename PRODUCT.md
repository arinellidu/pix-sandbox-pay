# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

**Primary — the integrator, debugging.** A developer running the emulator on localhost while building against a Brazilian PSP. They have a terminal open and their own SDK in the other window. Their job at the console: answer *did it land, and what happened to it?* — did the charge arrive, what status is it in, did the payment settle, did the webhook go out or fail.

**Secondary — the evaluator, watching.** Someone meeting the project through its README or demo GIF. The console is the visual proof that the emulator is real and that the lifecycle it claims to model actually runs. Confirmed as equally weighted with the primary user.

Also named in docs/DESIGN.md and still true: the *learner* (FedNow/SEPA engineer studying how Pix models charges, settlement and refunds) and the *tester* (QA/platform engineer who needs deterministic scenarios in CI). Neither drives the console today.

## Product Purpose

pix-sandbox emulates the full Pix instant-payment lifecycle — charge → EMV QR → payment → refund → webhook — in a single zero-config binary, so teams can build and test against Pix without a bank, credentials, or network access. Success is the 60-second loop: `docker run`, create a charge, pay it, watch the signed callback arrive.

## Positioning

API-compatible with BACEN's public API Pix surface, so real SDKs run unmodified — but local, deterministic by default (one seeded source, seed printed at boot), and with an append-only event log as the source of truth. No PSP sandbox offers reproducibility or failure injection; no mock server offers lifecycle fidelity. The event log is the differentiator a neighboring tool could not truthfully copy: every state the emulator reaches is a recorded transition, so the console shows what happened rather than what the current row says.

## Operating Context

- Runs on `localhost:8080` from `docker run` or `go run`, alongside the terminal that drives it with curl and the developer's own application.
- Confirmed scope: **local, single developer, low volume now — but no design decision may block larger volumes later.** A shared team server (Postgres, per docs/DESIGN.md) is a plausible future, not today's target.
- The console is read-only in this phase. Mutations happen through the API, on purpose: the console watches, the terminal acts.
- The demo GIF for the README is produced from this same surface.

## Capabilities and Constraints

- **Single static binary, zero-config.** SQLite embedded (modernc, CGO-free). Nothing may require an external service, CDN, or network fetch at runtime — the console's assets are embedded and served from the binary.
- Stack fixed by the project anchor: Go 1.26, chi, `templ` + htmx, stdlib-first. No heavy frameworks.
- Money is `int64` cents in the core; decimal strings only at the edge.
- Data available to the console today: charges (txid, status, amount, key, created/expires), payments (e2eId, txid, amount, refunded, horario), refunds, webhook registrations, and the append-only `events` log keyed by aggregate (`cob:{txid}`, `pix:{e2eId}`, `webhook:{chave}`).
- Domain vocabulary is BACEN's and stays untranslated: `cob`, `txid`, `e2eId`, `devolução`, `ATIVA`, `CONCLUIDA`, `EXPIRADA`, `DEVOLVIDO`, `pixCopiaECola`.
- Undecided: license (Apache-2.0 leaning), whether the console gains chaos controls in P2, `cobv` timing.

## Brand Commitments

- Repository is public-facing and **in English** — README, code, commits, and UI copy. The internal playbook may be Portuguese; the product surface is not.
- Name is lowercase `pix-sandbox`.
- No logo, wordmark, or brand palette exists yet. None may be invented as if it were established.

## Evidence on Hand

- `docs/DESIGN.md` — architecture, state machines, invariants INV-1..4, phased plan.
- Working API through S2, verified end to end against a real binary: charge → pay → refund → two signed callbacks.
- A real event log from that run, which is the console's actual content:
  `webhook.registered · cob.created · pix.received · cob.settled · webhook.delivered · pix.devolucao.requested · pix.devolucao.settled · webhook.delivered`
- No users, no testimonials, no adoption numbers, no benchmarks. None may be fabricated. The first named consumer is the sibling project `arinelli-pay`, which is real but not yet shipping.

## Product Principles

1. **The log is the truth.** The console reports recorded transitions, never inferred state. If something is not in the event log, the console does not claim it happened.
2. **Watch here, act there.** The console is read-only; the terminal drives. This keeps the demo honest and the surface small.
3. **Fidelity over convenience in vocabulary.** BACEN's terms are the product's terms. Explain them; do not rename them.
4. **Nothing leaves the box.** No external request, no CDN, no telemetry. A sandbox that phones home is not a sandbox.
5. **Determinism is a feature, and visible.** The seed, the identifiers, and the ordering are reproducible — the console should make that legible rather than hide it.

## Accessibility & Inclusion

No product-specific standard was established. Baseline expectations apply: keyboard reachability, visible focus, and status never carried by color alone — a status that only reads as green or red fails the primary user, who is scanning for the one row that went wrong.
