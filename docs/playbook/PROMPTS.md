# Playbook de Prompts — pix-sandbox (S0–S3)

## Regras de toda sessão (cole como preâmbulo se o Claude Code desviar)
- 1 prompt = 1 PR. Leia `CLAUDE.md` e `docs/DESIGN.md` antes de tocar em código.
- Nunca avance para o próximo P/S sem eu mandar. Nunca amplie escopo ("já que estou aqui...": não).
- Todo P termina com: testes verdes, lista de arquivos alterados, 1 linha no `CHANGELOG.md`, commit convencional (`feat:`, `fix:`, `docs:`).
- Dinheiro: `BigDecimal`/`NUMERIC(14,2)`. Documentos: só dígitos. Segredos: só via env, nunca em código.
- Se algo do design for ambíguo, PARE e me pergunte em vez de assumir.

---

# PARTE A — pix-sandbox (pré-requisito do Pay; repo próprio, Go)

## S0 — Bootstrap do emulador
```
Leia docs/DESIGN.md (pix-sandbox). Estamos no S0.

Objetivo: esqueleto executável do emulador.
Tarefas:
1. `go mod init github.com/arinelliquebec/pix-sandbox` (Go 1.26). Router chi. Sem frameworks pesados.
2. Endpoints: GET /health (200 {"status":"ok"}), POST /oauth/token (mock client_credentials, retorna bearer fake).
3. Store: SQLite embutido via modernc.org/sqlite (CGO-free), arquivo ./data/sandbox.db, criado no boot com tabela `events` (id, aggregate, type, payload JSON, created_at) e `charges` (txid PK, status, amount_cents INTEGER, chave, emv, created_at, expires_at).
4. Makefile: run, test, build (binário estático). Dockerfile multi-stage → imagem distroless.
5. README curto: o que é + como rodar em 1 comando.
Fora de escopo: EMV, pagamentos, webhooks.
Aceite: `make run` sobe em :8080; curl /health ok; `make test` verde.
```

## S1 — Cobrança + EMV BR Code
```
Leia docs/DESIGN.md. Estamos no S1. Base: S0 mergeado.

Objetivo: criar cobrança imediata compatível com API Pix e devolver payload EMV.
Tarefas:
1. POST /cob (e PUT /cob/{txid}): body {valor:{original:"10.00"}, chave, solicitacaoPagador?}. Se txid ausente no POST, gerar (26–35 chars alfanum). IDEMPOTENTE por txid: replay retorna a cobrança original (INV-2).
2. GET /cob/{txid}: status ATIVA|CONCLUIDA|EXPIRADA (+ campos da spec: calendario, valor, chave, location).
3. Gerador EMV BR Code dinâmico: TLV com merchant account info (gui br.gov.bcb.pix), valor, txid no campo 62-05, CRC16-CCITT correto no campo 63. Testes de tabela validando o CRC contra vetores fixos.
4. GET /cob/{txid}/qrcode → {"qrcode": "<payload EMV>", "imagemQrcode": null} (imagem é fase 2).
5. Toda mutação grava evento em `events` (append-only, INV-3).
Aceite: criar cobrança via curl retorna EMV cujo CRC valida; replay do mesmo txid não duplica; testes do TLV/CRC verdes.
```

## S2 — Pagamento simulado + webhook
```
Leia docs/DESIGN.md. Estamos no S2. Base: S1.

Objetivo: fechar o demo loop — pagar e notificar.
Tarefas:
1. POST /sandbox/pay {txid}: gera e2eId (E + ISPB fake + timestamp + seq), marca charge CONCLUIDA, grava evento pix.received.
2. GET /pix/{e2eId} conforme spec (valor, horario, txid, infoPagador).
3. PUT /webhook/{chave} registra URL de callback; GET idem para inspecionar.
4. Dispatcher de webhook: POST na URL registrada com body {pix:[...]}, header `X-Signature: hmac-sha256(body, WEBHOOK_SECRET env)`, retry exponencial 3x (1s/5s/25s), resultado logado em `events`.
5. Devolução: PUT /pix/{e2eId}/devolucao/{id} (total apenas em v1) → status DEVOLVIDO + evento + webhook.
Fora de escopo: chaos API, clock virtual, multi-PSP (fases seguintes).
Aceite: fluxo curl completo do §7 do DESIGN roda ponta a ponta com webhook recebido num http-echo local; INV-2/3/4 testados.
```

## S3 — Console + release P0
```
Leia docs/DESIGN.md. Estamos no S3. Base: S2.

Objetivo: shippar o P0.
Tarefas:
1. Console embutido (templ + htmx, servido em /console): lista de cobranças com status e timeline de `events` por txid. Read-only.
2. README completo: badges, demo loop de 60s com os curls, seção "Why this exists" (2 parágrafos, EN), arquitetura em Mermaid (copiar do DESIGN), roadmap P1–P3.
3. GitHub Actions: build + test + push da imagem (ghcr.io) em tag.
4. Instruções para GIF do demo (vhs ou terminalizer) no README.
Aceite: `docker run -p 8080:8080 ghcr.io/arinelliquebec/pix-sandbox` roda o demo loop; console mostra a cobrança paga; CI verde. → TAG v0.1.0 e o P00 do Pay destrava.
```

---

