package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arinellidu/pix-sandbox-pay/internal/api"
	"github.com/arinellidu/pix-sandbox-pay/internal/emv"
	"github.com/arinellidu/pix-sandbox-pay/internal/store"
)

// cobBody is the response shape the API Pix specifies for a charge.
type cobBody struct {
	Calendario struct {
		Criacao   string `json:"criacao"`
		Expiracao int64  `json:"expiracao"`
	} `json:"calendario"`
	TxID    string `json:"txid"`
	Revisao int64  `json:"revisao"`
	Loc     struct {
		ID       int64  `json:"id"`
		Location string `json:"location"`
		TipoCob  string `json:"tipoCob"`
	} `json:"loc"`
	Location string `json:"location"`
	Status   string `json:"status"`
	Devedor  *struct {
		CPF  string `json:"cpf"`
		CNPJ string `json:"cnpj"`
		Nome string `json:"nome"`
	} `json:"devedor"`
	Valor struct {
		Original string `json:"original"`
	} `json:"valor"`
	Chave              string `json:"chave"`
	SolicitacaoPagador string `json:"solicitacaoPagador"`
	PixCopiaECola      string `json:"pixCopiaECola"`
}

type problemBody struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail"`
	Violacoes []struct {
		Razao       string `json:"razao"`
		Propriedade string `json:"propriedade"`
	} `json:"violacoes"`
}

func postJSON(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return do(t, h, req)
}

func putJSON(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return do(t, h, req)
}

func decodeCob(t *testing.T, rec *httptest.ResponseRecorder) cobBody {
	t.Helper()
	var body cobBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode cob: %v", err)
	}
	return body
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) problemBody {
	t.Helper()
	var body problemBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return body
}

const validTxID = "abc123def456ghi789jkl012mno345"

func TestCreateCobGeneratesTxID(t *testing.T) {
	h, _ := newServer(t)

	rec := postJSON(t, h, "/cob", `{"valor":{"original":"10.00"},"chave":"dev@example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rec.Code, rec.Body)
	}

	body := decodeCob(t, rec)
	if len(body.TxID) < 26 || len(body.TxID) > 35 {
		t.Errorf("generated txid %q has length %d, want 26..35", body.TxID, len(body.TxID))
	}
	for _, r := range body.TxID {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			t.Fatalf("generated txid %q is not alphanumeric", body.TxID)
		}
	}
	if body.Status != "ATIVA" {
		t.Errorf("status = %q, want ATIVA", body.Status)
	}
	if body.Valor.Original != "10.00" {
		t.Errorf("valor.original = %q, want 10.00", body.Valor.Original)
	}
	if body.Chave != "dev@example.com" {
		t.Errorf("chave = %q", body.Chave)
	}
	if body.Calendario.Expiracao != 86400 {
		t.Errorf("calendario.expiracao = %d, want the default 86400", body.Calendario.Expiracao)
	}
	if body.Calendario.Criacao == "" {
		t.Error("calendario.criacao is empty")
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", body.Calendario.Criacao); err != nil {
		t.Errorf("calendario.criacao %q is not in the API format: %v", body.Calendario.Criacao, err)
	}
	if body.Loc.ID == 0 || body.Loc.TipoCob != "cob" {
		t.Errorf("loc = %+v", body.Loc)
	}
	if body.Location != body.Loc.Location {
		t.Errorf("location %q and loc.location %q disagree", body.Location, body.Loc.Location)
	}
	if body.Revisao != 0 {
		t.Errorf("revisao = %d, want 0", body.Revisao)
	}
}

// The acceptance criterion for S1: the payload a charge hands back carries a
// CRC that validates.
func TestCreateCobReturnsValidEMV(t *testing.T) {
	h, _ := newServer(t)

	rec := postJSON(t, h, "/cob", `{"valor":{"original":"10.00"},"chave":"dev@example.com"}`)
	body := decodeCob(t, rec)

	if body.PixCopiaECola == "" {
		t.Fatal("pixCopiaECola is empty")
	}
	if err := emv.Verify(body.PixCopiaECola); err != nil {
		t.Fatalf("CRC does not validate: %v", err)
	}

	fields, err := emv.Parse(body.PixCopiaECola)
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if amount, _ := emv.Find(fields, emv.FieldAmount); amount != "10.00" {
		t.Errorf("field 54 = %q, want 10.00", amount)
	}

	account, _ := emv.Find(fields, emv.FieldMerchantAccount)
	sub, err := emv.Parse(account)
	if err != nil {
		t.Fatalf("parse merchant account: %v", err)
	}
	if gui, _ := emv.Find(sub, emv.SubGUI); gui != emv.GUI {
		t.Errorf("GUI = %q, want %q", gui, emv.GUI)
	}
	if key, _ := emv.Find(sub, emv.SubKey); key != "dev@example.com" {
		t.Errorf("field 26-01 = %q, want the pix key", key)
	}

	additional, _ := emv.Find(fields, emv.FieldAdditionalData)
	sub, err = emv.Parse(additional)
	if err != nil {
		t.Fatalf("parse additional data: %v", err)
	}
	// 62-05 caps at 25 chars and a cob txid is 26-35: the payload must carry
	// "***" instead of a field real readers reject on length.
	if txid, _ := emv.Find(sub, emv.SubTxID); txid != emv.NoTxID {
		t.Errorf("field 62-05 = %q, want %q", txid, emv.NoTxID)
	}
}

func TestCreateCobWithSuppliedTxID(t *testing.T) {
	h, _ := newServer(t)

	rec := postJSON(t, h, "/cob",
		fmt.Sprintf(`{"txid":%q,"valor":{"original":"10.00"},"chave":"dev@example.com"}`, validTxID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rec.Code, rec.Body)
	}
	if got := decodeCob(t, rec).TxID; got != validTxID {
		t.Errorf("txid = %q, want %q", got, validTxID)
	}
}

func TestPutCobUsesPathTxID(t *testing.T) {
	h, _ := newServer(t)

	rec := putJSON(t, h, "/cob/"+validTxID, `{"valor":{"original":"25.50"},"chave":"dev@example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rec.Code, rec.Body)
	}

	body := decodeCob(t, rec)
	if body.TxID != validTxID {
		t.Errorf("txid = %q, want %q", body.TxID, validTxID)
	}
	if body.Valor.Original != "25.50" {
		t.Errorf("valor.original = %q, want 25.50", body.Valor.Original)
	}
}

func TestPutCobRejectsMismatchedTxID(t *testing.T) {
	h, _ := newServer(t)

	rec := putJSON(t, h, "/cob/"+validTxID,
		`{"txid":"zzz123def456ghi789jkl012mno345","valor":{"original":"10.00"},"chave":"dev@example.com"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if got := decodeProblem(t, rec).Violacoes; len(got) == 0 || got[0].Propriedade != "txid" {
		t.Errorf("violacoes = %+v, want one about txid", got)
	}
}

// INV-2: replaying a create with the same txid returns the original charge and
// creates nothing.
func TestCreateCobIsIdempotent(t *testing.T) {
	h, st := newServer(t)

	const first = `{"valor":{"original":"10.00"},"chave":"dev@example.com"}`
	rec := putJSON(t, h, "/cob/"+validTxID, first)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201 (body: %s)", rec.Code, rec.Body)
	}
	original := decodeCob(t, rec)

	// Replay with different content: the stored charge must win.
	rec = putJSON(t, h, "/cob/"+validTxID, `{"valor":{"original":"999.99"},"chave":"other@example.com"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}

	replay := decodeCob(t, rec)
	if replay.Valor.Original != original.Valor.Original {
		t.Errorf("replay valor = %q, want the original %q", replay.Valor.Original, original.Valor.Original)
	}
	if replay.Chave != original.Chave {
		t.Errorf("replay chave = %q, want the original %q", replay.Chave, original.Chave)
	}
	if replay.PixCopiaECola != original.PixCopiaECola {
		t.Error("replay returned a different BR Code")
	}

	var rows int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM charges`).Scan(&rows); err != nil {
		t.Fatalf("count charges: %v", err)
	}
	if rows != 1 {
		t.Errorf("charges rows = %d, want 1", rows)
	}

	events, err := st.EventsByAggregate(t.Context(), store.ChargeAggregate(validTxID))
	if err != nil {
		t.Fatalf("EventsByAggregate: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("events = %d, want 1", len(events))
	}
}

// A restart rewinds the rng but not the database. The first minted txid of
// the new run collides with the first of the old one; the server must mint
// past the collision and create the charge asked for, not replay the old one.
func TestCreateCobMintsPastRestartCollision(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "data", "sandbox.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	run1 := newServerOn(t, st, api.Config{})
	rec := postJSON(t, run1, "/cob", `{"valor":{"original":"10.00"},"chave":"dev@example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("run 1 status = %d, want 201 (body: %s)", rec.Code, rec.Body)
	}
	first := decodeCob(t, rec)

	run2 := newServerOn(t, st, api.Config{})
	rec = postJSON(t, run2, "/cob", `{"valor":{"original":"25.00"},"chave":"other@example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("run 2 status = %d, want 201 (body: %s)", rec.Code, rec.Body)
	}
	second := decodeCob(t, rec)

	if second.TxID == first.TxID {
		t.Fatalf("run 2 reused txid %q from run 1", first.TxID)
	}
	if second.Valor.Original != "25.00" || second.Chave != "other@example.com" {
		t.Errorf("run 2 charge = %q/%q, want the requested 25.00/other@example.com",
			second.Valor.Original, second.Chave)
	}
}

// INV-3: creating a charge lands in the append-only log.
func TestCreateCobLogsEvent(t *testing.T) {
	h, st := newServer(t)

	rec := postJSON(t, h, "/cob", `{"valor":{"original":"10.00"},"chave":"dev@example.com"}`)
	body := decodeCob(t, rec)

	events, err := st.EventsByAggregate(t.Context(), store.ChargeAggregate(body.TxID))
	if err != nil {
		t.Fatalf("EventsByAggregate: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Type != store.EventChargeCreated {
		t.Errorf("event type = %q, want %q", events[0].Type, store.EventChargeCreated)
	}

	var payload struct {
		AmountCents int64  `json:"amount_cents"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.AmountCents != 1000 {
		t.Errorf("amount_cents = %d, want 1000: the log speaks in cents", payload.AmountCents)
	}
	if payload.Status != "ATIVA" {
		t.Errorf("status = %q, want ATIVA", payload.Status)
	}
}

func TestCreateCobWithDevedorAndSolicitacao(t *testing.T) {
	h, _ := newServer(t)

	rec := postJSON(t, h, "/cob", `{
		"calendario": {"expiracao": 3600},
		"devedor": {"cpf": "12345678909", "nome": "Francisco da Silva"},
		"valor": {"original": "10.00"},
		"chave": "dev@example.com",
		"solicitacaoPagador": "Servico realizado."
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rec.Code, rec.Body)
	}

	body := decodeCob(t, rec)
	if body.Calendario.Expiracao != 3600 {
		t.Errorf("expiracao = %d, want 3600", body.Calendario.Expiracao)
	}
	if body.Devedor == nil || body.Devedor.CPF != "12345678909" || body.Devedor.Nome != "Francisco da Silva" {
		t.Errorf("devedor = %+v", body.Devedor)
	}
	if body.SolicitacaoPagador != "Servico realizado." {
		t.Errorf("solicitacaoPagador = %q", body.SolicitacaoPagador)
	}
}

func TestCreateCobValidation(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantProperty string
	}{
		{
			name:         "missing valor",
			body:         `{"chave":"dev@example.com"}`,
			wantProperty: "valor.original",
		},
		{
			name:         "amount without decimals",
			body:         `{"valor":{"original":"10"},"chave":"dev@example.com"}`,
			wantProperty: "valor.original",
		},
		{
			name:         "amount with comma",
			body:         `{"valor":{"original":"10,00"},"chave":"dev@example.com"}`,
			wantProperty: "valor.original",
		},
		{
			name:         "amount zero",
			body:         `{"valor":{"original":"0.00"},"chave":"dev@example.com"}`,
			wantProperty: "valor.original",
		},
		{
			name:         "missing chave",
			body:         `{"valor":{"original":"10.00"}}`,
			wantProperty: "chave",
		},
		{
			name:         "txid too short",
			body:         `{"txid":"short","valor":{"original":"10.00"},"chave":"dev@example.com"}`,
			wantProperty: "txid",
		},
		{
			name:         "txid not alphanumeric",
			body:         `{"txid":"abc-123def456ghi789jkl012mno","valor":{"original":"10.00"},"chave":"dev@example.com"}`,
			wantProperty: "txid",
		},
		{
			name:         "expiracao zero",
			body:         `{"calendario":{"expiracao":0},"valor":{"original":"10.00"},"chave":"dev@example.com"}`,
			wantProperty: "calendario.expiracao",
		},
		{
			name:         "expiracao negative",
			body:         `{"calendario":{"expiracao":-1},"valor":{"original":"10.00"},"chave":"dev@example.com"}`,
			wantProperty: "calendario.expiracao",
		},
		{
			name:         "devedor without name",
			body:         `{"devedor":{"cpf":"12345678909"},"valor":{"original":"10.00"},"chave":"dev@example.com"}`,
			wantProperty: "devedor.nome",
		},
		{
			name:         "cpf with punctuation",
			body:         `{"devedor":{"cpf":"123.456.789-09","nome":"Fulano"},"valor":{"original":"10.00"},"chave":"dev@example.com"}`,
			wantProperty: "devedor.cpf",
		},
		{
			name:         "both cpf and cnpj",
			body:         `{"devedor":{"cpf":"12345678909","cnpj":"12345678000199","nome":"Fulano"},"valor":{"original":"10.00"},"chave":"dev@example.com"}`,
			wantProperty: "devedor",
		},
		{
			name:         "devedor without document",
			body:         `{"devedor":{"nome":"Fulano"},"valor":{"original":"10.00"},"chave":"dev@example.com"}`,
			wantProperty: "devedor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, st := newServer(t)

			rec := postJSON(t, h, "/cob", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("content-type = %q, want application/problem+json", ct)
			}

			body := decodeProblem(t, rec)
			if body.Status != http.StatusBadRequest {
				t.Errorf("problem.status = %d, want 400", body.Status)
			}
			if !strings.HasPrefix(body.Type, "https://pix.bcb.gov.br/api/v2/error/") {
				t.Errorf("problem.type = %q", body.Type)
			}

			var found bool
			for _, v := range body.Violacoes {
				if v.Propriedade == tt.wantProperty {
					found = true
				}
			}
			if !found {
				t.Errorf("violacoes = %+v, want one about %q", body.Violacoes, tt.wantProperty)
			}

			// A rejected charge must leave nothing behind.
			var rows int
			if err := st.DB().QueryRow(`SELECT COUNT(*) FROM charges`).Scan(&rows); err != nil {
				t.Fatalf("count charges: %v", err)
			}
			if rows != 0 {
				t.Errorf("charges rows = %d after a rejected request, want 0", rows)
			}
		})
	}
}

func TestCreateCobReportsEveryViolation(t *testing.T) {
	h, _ := newServer(t)

	rec := postJSON(t, h, "/cob", `{"txid":"short","valor":{"original":"nope"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	body := decodeProblem(t, rec)
	if len(body.Violacoes) < 3 {
		t.Errorf("violacoes = %+v, want txid, valor.original and chave reported together", body.Violacoes)
	}
}

func TestCreateCobRejectsMalformedJSON(t *testing.T) {
	h, _ := newServer(t)

	rec := postJSON(t, h, "/cob", `{"valor":`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if got := decodeProblem(t, rec).Type; !strings.HasSuffix(got, "RequisicaoInvalida") {
		t.Errorf("problem.type = %q, want RequisicaoInvalida", got)
	}
}

func TestGetCob(t *testing.T) {
	h, _ := newServer(t)

	created := decodeCob(t, putJSON(t, h, "/cob/"+validTxID,
		`{"valor":{"original":"10.00"},"chave":"dev@example.com"}`))

	rec := do(t, h, httptest.NewRequest(http.MethodGet, "/cob/"+validTxID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}

	got := decodeCob(t, rec)
	if got.TxID != created.TxID || got.PixCopiaECola != created.PixCopiaECola {
		t.Errorf("GET returned a different charge:\n%+v\n%+v", got, created)
	}
	if got.Status != "ATIVA" {
		t.Errorf("status = %q, want ATIVA", got.Status)
	}
}

func TestGetCobNotFound(t *testing.T) {
	h, _ := newServer(t)

	rec := do(t, h, httptest.NewRequest(http.MethodGet, "/cob/"+validTxID, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got := decodeProblem(t, rec).Type; !strings.HasSuffix(got, "CobNaoEncontrada") {
		t.Errorf("problem.type = %q, want CobNaoEncontrada", got)
	}
}

func TestGetCobRejectsMalformedTxID(t *testing.T) {
	h, _ := newServer(t)

	rec := do(t, h, httptest.NewRequest(http.MethodGet, "/cob/short", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestGetCobQRCode(t *testing.T) {
	h, _ := newServer(t)

	created := decodeCob(t, putJSON(t, h, "/cob/"+validTxID,
		`{"valor":{"original":"10.00"},"chave":"dev@example.com"}`))

	rec := do(t, h, httptest.NewRequest(http.MethodGet, "/cob/"+validTxID+"/qrcode", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}

	var body struct {
		QRCode       string  `json:"qrcode"`
		ImagemQrcode *string `json:"imagemQrcode"`
	}
	raw := rec.Body.Bytes()
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.QRCode != created.PixCopiaECola {
		t.Errorf("qrcode = %q, want the charge payload", body.QRCode)
	}
	if err := emv.Verify(body.QRCode); err != nil {
		t.Errorf("payload CRC does not validate: %v", err)
	}
	if body.ImagemQrcode != nil {
		t.Errorf("imagemQrcode = %v, want null in this phase", *body.ImagemQrcode)
	}
	if !strings.Contains(string(raw), `"imagemQrcode":null`) {
		t.Errorf("imagemQrcode should be present and null, body was %s", raw)
	}
}

func TestGetCobQRCodeNotFound(t *testing.T) {
	h, _ := newServer(t)

	rec := do(t, h, httptest.NewRequest(http.MethodGet, "/cob/"+validTxID+"/qrcode", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// A charge read past its window reports EXPIRADA, and the transition is in the
// log rather than merely inferred by the reader.
func TestGetCobExpires(t *testing.T) {
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	now := base
	h, st := newServerAt(t, func() time.Time { return now })

	rec := putJSON(t, h, "/cob/"+validTxID,
		`{"calendario":{"expiracao":3600},"valor":{"original":"10.00"},"chave":"dev@example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d (body: %s)", rec.Code, rec.Body)
	}

	// Inside the window.
	now = base.Add(59 * time.Minute)
	got := decodeCob(t, do(t, h, httptest.NewRequest(http.MethodGet, "/cob/"+validTxID, nil)))
	if got.Status != "ATIVA" {
		t.Errorf("status inside the window = %q, want ATIVA", got.Status)
	}

	// Past it.
	now = base.Add(time.Hour + time.Second)
	got = decodeCob(t, do(t, h, httptest.NewRequest(http.MethodGet, "/cob/"+validTxID, nil)))
	if got.Status != "EXPIRADA" {
		t.Errorf("status past the window = %q, want EXPIRADA", got.Status)
	}

	// Reading again must not log the transition twice.
	now = base.Add(2 * time.Hour)
	do(t, h, httptest.NewRequest(http.MethodGet, "/cob/"+validTxID, nil))

	events, err := st.EventsByAggregate(t.Context(), store.ChargeAggregate(validTxID))
	if err != nil {
		t.Fatalf("EventsByAggregate: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (created, expired)", len(events))
	}
	if events[1].Type != store.EventChargeExpired {
		t.Errorf("second event = %q, want %q", events[1].Type, store.EventChargeExpired)
	}
}

// An expired charge stays readable, payload and all.
func TestExpiredChargeKeepsItsPayload(t *testing.T) {
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	now := base
	h, _ := newServerAt(t, func() time.Time { return now })

	created := decodeCob(t, putJSON(t, h, "/cob/"+validTxID,
		`{"calendario":{"expiracao":60},"valor":{"original":"10.00"},"chave":"dev@example.com"}`))

	now = base.Add(time.Hour)
	got := decodeCob(t, do(t, h, httptest.NewRequest(http.MethodGet, "/cob/"+validTxID, nil)))
	if got.Status != "EXPIRADA" {
		t.Fatalf("status = %q, want EXPIRADA", got.Status)
	}
	if got.PixCopiaECola != created.PixCopiaECola {
		t.Error("expired charge lost its BR Code")
	}
}

// Same seed, same generated txid: a sandbox run is reproducible (ADR-007).
func TestGeneratedTxIDIsSeeded(t *testing.T) {
	const body = `{"valor":{"original":"10.00"},"chave":"dev@example.com"}`

	first, _ := newServer(t)
	second, _ := newServer(t)

	a := decodeCob(t, postJSON(t, first, "/cob", body)).TxID
	b := decodeCob(t, postJSON(t, second, "/cob", body)).TxID
	if a != b {
		t.Errorf("txids from equally seeded servers differ: %q and %q", a, b)
	}
}

func TestAmountsRoundTripThroughCents(t *testing.T) {
	for _, amount := range []string{"0.01", "0.99", "10.00", "1234.56", "9999999999.99"} {
		t.Run(amount, func(t *testing.T) {
			h, _ := newServer(t)

			rec := postJSON(t, h, "/cob",
				fmt.Sprintf(`{"valor":{"original":%q},"chave":"dev@example.com"}`, amount))
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201 (body: %s)", rec.Code, rec.Body)
			}

			body := decodeCob(t, rec)
			if body.Valor.Original != amount {
				t.Errorf("valor.original = %q, want %q", body.Valor.Original, amount)
			}

			fields, err := emv.Parse(body.PixCopiaECola)
			if err != nil {
				t.Fatalf("parse payload: %v", err)
			}
			if got, _ := emv.Find(fields, emv.FieldAmount); got != amount {
				t.Errorf("field 54 = %q, want %q", got, amount)
			}
		})
	}
}
