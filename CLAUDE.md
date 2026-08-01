# CLAUDE.md — pix-sandbox (session anchor)

Leia este arquivo e docs/DESIGN.md antes de qualquer alteração. Siga docs/playbook/PROMPTS.md (S0–S3) na ordem — 1 prompt = 1 PR.

## O que é
Emulador drop-in do ciclo de vida Pix (cobrança → QR EMV → pagamento → devolução → webhook) em binário único, compatível com a superfície da API Pix do BACEN. Ferramenta para terceiros: times integrando com PSPs brasileiros e times FedNow/SEPA estudando o Pix.

## Invariantes (property-tested; nunca viole)
- **INV-1 Conservação:** Σ dos saldos entre PSPs virtuais é constante em qualquer liquidação (aplica a partir do multi-PSP, fase P3).
- **INV-2 Unicidade/idempotência:** txid e e2eId únicos; replay de POST /cob com o mesmo txid retorna a cobrança original.
- **INV-3 Sem estados pulados:** toda transição vai para o event log append-only; projeções reconstruídas do log sempre concordam com o estado.
- **INV-4 Teto de devolução:** Σ devoluções de um pagamento ≤ valor liquidado.

## Regras de engenharia
- **Binário único, zero-config:** `docker run` → funcionando. SQLite embutido (modernc, CGO-free); nada de dependência externa no start.
- **Determinístico por padrão:** toda aleatoriedade passa por uma fonte seedada (seed impressa no boot). Falha só via Chaos API (fase P2).
- **Stack:** Go 1.26, chi, modernc.org/sqlite, templ + htmx (console embutido). Sem frameworks pesados. Stdlib-first.
- **Dinheiro em centavos (int64)** no core; formatação só na borda.
- **Repo público em inglês:** README, commits e código EN; este anchor e o playbook podem ser PT.
- Delivery AI-assisted com revisão humana em todo merge; este documento e o DESIGN são a spec (I7 da casa: LLM drafts, deterministic code executes).

## Fases
S0 bootstrap → S1 cob+EMV → S2 pay+webhook+devolução → S3 console+release **v0.1.0** (destrava o P00 do arinelli-pay, que consome este emulador como primeiro cliente).
