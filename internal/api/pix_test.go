package api_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/arinelliquebec/pix-sandbox/internal/api"
	"github.com/arinelliquebec/pix-sandbox/internal/store"
	"github.com/arinelliquebec/pix-sandbox/internal/webhook"
)

// pixBody is the `Pix` resource the API Pix specifies.
type pixBody struct {
	EndToEndID  string `json:"endToEndId"`
	TxID        string `json:"txid"`
	Valor       string `json:"valor"`
	Chave       string `json:"chave"`
	Horario     string `json:"horario"`
	InfoPagador string `json:"infoPagador"`
	Devolucoes  []struct {
		ID      string `json:"id"`
		RtrID   string `json:"rtrId"`
		Valor   string `json:"valor"`
		Horario struct {
			Solicitacao string `json:"solicitacao"`
			Liquidacao  string `json:"liquidacao"`
		} `json:"horario"`
		Status string `json:"status"`
	} `json:"devolucoes"`
}

func decodePix(t *testing.T, rec *httptest.ResponseRecorder) pixBody {
	t.Helper()
	var body pixBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode pix: %v", err)
	}
	return body
}

// payCharge creates a charge and pays it, returning the resulting payment.
func payCharge(t *testing.T, h http.Handler, txid string) pixBody {
	t.Helper()

	rec := putJSON(t, h, "/cob/"+txid, `{"valor":{"original":"10.00"},"chave":"dev@example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create charge: status = %d (body: %s)", rec.Code, rec.Body)
	}

	rec = postJSON(t, h, "/sandbox/pay", fmt.Sprintf(`{"txid":%q,"infoPagador":"Coffee"}`, txid))
	if rec.Code != http.StatusCreated {
		t.Fatalf("pay: status = %d (body: %s)", rec.Code, rec.Body)
	}
	return decodePix(t, rec)
}

// callback is one delivery seen by the receiver under test.
type callback struct {
	body      []byte
	signature string
}

// echoServer stands in for the payee's endpoint.
func echoServer(t *testing.T) (*httptest.Server, chan callback) {
	t.Helper()
	calls := make(chan callback, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls <- callback{body: body, signature: r.Header.Get(webhook.SignatureHeader)}
	}))
	t.Cleanup(server.Close)
	return server, calls
}

func awaitCallback(t *testing.T, calls chan callback) webhookBody {
	t.Helper()
	select {
	case call := <-calls:
		var body webhookBody
		if err := json.Unmarshal(call.body, &body); err != nil {
			t.Fatalf("decode callback: %v (body: %s)", err, call.body)
		}
		body.raw = call.body
		body.signature = call.signature
		return body
	case <-time.After(5 * time.Second):
		t.Fatal("the webhook was never delivered")
		return webhookBody{}
	}
}

type webhookBody struct {
	Pix []pixBody `json:"pix"`

	raw       []byte
	signature string
}

const testSecret = "test-secret"

// fastWebhooks keeps the retry schedule out of the way of the test clock.
func fastWebhooks() api.Config {
	return api.Config{Webhook: webhook.Config{
		Secret:  testSecret,
		Backoff: []time.Duration{time.Millisecond},
	}}
}

// The acceptance criterion for S2: §7 of the design, end to end — charge, pay,
// signed callback at the payee's endpoint.
func TestDemoLoopEndToEnd(t *testing.T) {
	h, st := newServerWith(t, fastWebhooks())
	echo, calls := echoServer(t)

	rec := putJSON(t, h, "/webhook/dev@example.com",
		fmt.Sprintf(`{"webhookUrl":%q}`, echo.URL))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register webhook: status = %d (body: %s)", rec.Code, rec.Body)
	}

	created := decodeCob(t, postJSON(t, h, "/cob",
		`{"valor":{"original":"10.00"},"chave":"dev@example.com"}`))

	rec = postJSON(t, h, "/sandbox/pay", fmt.Sprintf(`{"txid":%q}`, created.TxID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("pay: status = %d (body: %s)", rec.Code, rec.Body)
	}
	payment := decodePix(t, rec)
	if payment.TxID != created.TxID || payment.Valor != "10.00" {
		t.Errorf("payment = %+v", payment)
	}

	delivered := awaitCallback(t, calls)
	if len(delivered.Pix) != 1 {
		t.Fatalf("callback carried %d pix, want 1 (body: %s)", len(delivered.Pix), delivered.raw)
	}
	if got := delivered.Pix[0].EndToEndID; got != payment.EndToEndID {
		t.Errorf("callback e2eid = %q, want %q", got, payment.EndToEndID)
	}
	if want := webhook.Sign(testSecret, delivered.raw); delivered.signature != want {
		t.Errorf("signature = %q, want %q", delivered.signature, want)
	}

	// The charge is closed and the callback is on the payment's timeline.
	if got := decodeCob(t, do(t, h, httptest.NewRequest(http.MethodGet, "/cob/"+created.TxID, nil))); got.Status != "CONCLUIDA" {
		t.Errorf("charge status = %q, want CONCLUIDA", got.Status)
	}
	waitForEvent(t, st, store.PaymentAggregate(payment.EndToEndID), store.EventWebhookDelivered)
}

func TestSandboxPayMintsASpecCompliantE2EID(t *testing.T) {
	h, _ := newServer(t)

	payment := payCharge(t, h, validTxID)
	if len(payment.EndToEndID) != 32 {
		t.Fatalf("e2eid %q has length %d, want 32", payment.EndToEndID, len(payment.EndToEndID))
	}
	if payment.EndToEndID[0] != 'E' {
		t.Errorf("e2eid = %q, want an E prefix", payment.EndToEndID)
	}
	for i, r := range payment.EndToEndID[1:21] {
		if r < '0' || r > '9' {
			t.Fatalf("e2eid %q has a non-digit at %d: %q", payment.EndToEndID, i+1, r)
		}
	}
	if payment.InfoPagador != "Coffee" {
		t.Errorf("infoPagador = %q, want Coffee", payment.InfoPagador)
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", payment.Horario); err != nil {
		t.Errorf("horario %q is not in the API format: %v", payment.Horario, err)
	}
}

// INV-2: a charge settles once. The refusal names the payment that already
// exists rather than minting a second one.
func TestSandboxPayRefusesToPayTwice(t *testing.T) {
	h, st := newServer(t)

	first := payCharge(t, h, validTxID)

	rec := postJSON(t, h, "/sandbox/pay", fmt.Sprintf(`{"txid":%q}`, validTxID))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body: %s)", rec.Code, rec.Body)
	}
	if detail := decodeProblem(t, rec).Detail; !strings.Contains(detail, first.EndToEndID) {
		t.Errorf("detail = %q, want the original e2eid named", detail)
	}

	var rows int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&rows); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if rows != 1 {
		t.Errorf("payments rows = %d, want 1", rows)
	}
}

func TestSandboxPayRefusesAnExpiredCharge(t *testing.T) {
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	now := base
	cfg := fastWebhooks()
	cfg.Now = func() time.Time { return now }
	h, st := newServerWith(t, cfg)

	putJSON(t, h, "/cob/"+validTxID,
		`{"calendario":{"expiracao":60},"valor":{"original":"10.00"},"chave":"dev@example.com"}`)

	now = base.Add(time.Hour)
	rec := postJSON(t, h, "/sandbox/pay", fmt.Sprintf(`{"txid":%q}`, validTxID))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body)
	}
	if detail := decodeProblem(t, rec).Detail; !strings.Contains(detail, "EXPIRADA") {
		t.Errorf("detail = %q, want the state named", detail)
	}

	// INV-3: the refusal is grounded in a recorded transition, not a guess.
	events, err := st.EventsByAggregate(t.Context(), store.ChargeAggregate(validTxID))
	if err != nil {
		t.Fatalf("EventsByAggregate: %v", err)
	}
	if len(events) != 2 || events[1].Type != store.EventChargeExpired {
		t.Errorf("events = %+v, want created then expired", events)
	}
}

func TestSandboxPayValidation(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{name: "missing txid", body: `{}`, wantCode: http.StatusBadRequest},
		{name: "malformed txid", body: `{"txid":"short"}`, wantCode: http.StatusBadRequest},
		{
			name:     "unknown charge",
			body:     fmt.Sprintf(`{"txid":%q}`, validTxID),
			wantCode: http.StatusNotFound,
		},
		{
			name:     "infoPagador too long",
			body:     fmt.Sprintf(`{"txid":%q,"infoPagador":%q}`, validTxID, strings.Repeat("x", 141)),
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newServer(t)

			rec := postJSON(t, h, "/sandbox/pay", tt.body)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantCode, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("content-type = %q, want application/problem+json", ct)
			}
		})
	}
}

func TestGetPix(t *testing.T) {
	h, _ := newServer(t)

	payment := payCharge(t, h, validTxID)

	rec := do(t, h, httptest.NewRequest(http.MethodGet, "/pix/"+payment.EndToEndID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}

	got := decodePix(t, rec)
	if got.EndToEndID != payment.EndToEndID || got.TxID != validTxID {
		t.Errorf("pix = %+v", got)
	}
	if got.Valor != "10.00" || got.Chave != "dev@example.com" {
		t.Errorf("pix = %+v", got)
	}
	if len(got.Devolucoes) != 0 {
		t.Errorf("devolucoes = %+v, want none", got.Devolucoes)
	}
}

func TestGetPixNotFound(t *testing.T) {
	h, _ := newServer(t)

	for _, path := range []string{
		"/pix/E12345678202607311204x7k2q90000f", // well-formed, unknown
		"/pix/nope",                             // malformed
	} {
		rec := do(t, h, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, rec.Code)
		}
		if got := decodeProblem(t, rec).Type; !strings.HasSuffix(got, "PixNaoEncontrado") {
			t.Errorf("GET %s: problem.type = %q", path, got)
		}
	}
}

func TestDevolucaoRefundsAndNotifies(t *testing.T) {
	h, st := newServerWith(t, fastWebhooks())
	echo, calls := echoServer(t)

	putJSON(t, h, "/webhook/dev@example.com", fmt.Sprintf(`{"webhookUrl":%q}`, echo.URL))
	payment := payCharge(t, h, validTxID)
	awaitCallback(t, calls) // the payment's own notification

	rec := putJSON(t, h, "/pix/"+payment.EndToEndID+"/devolucao/dev1", `{"valor":"10.00"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rec.Code, rec.Body)
	}

	var refund struct {
		ID      string `json:"id"`
		RtrID   string `json:"rtrId"`
		Valor   string `json:"valor"`
		Status  string `json:"status"`
		Horario struct {
			Solicitacao string `json:"solicitacao"`
			Liquidacao  string `json:"liquidacao"`
		} `json:"horario"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&refund); err != nil {
		t.Fatalf("decode devolucao: %v", err)
	}
	if refund.Status != "DEVOLVIDO" {
		t.Errorf("status = %q, want DEVOLVIDO", refund.Status)
	}
	if refund.Valor != "10.00" || refund.ID != "dev1" {
		t.Errorf("devolucao = %+v", refund)
	}
	if len(refund.RtrID) != 32 || refund.RtrID[0] != 'D' {
		t.Errorf("rtrId = %q, want a 32-character D identifier", refund.RtrID)
	}
	if refund.Horario.Liquidacao == "" {
		t.Error("horario.liquidacao is empty on a settled refund")
	}

	// The refund shows up on the payment and in the callback.
	got := decodePix(t, do(t, h, httptest.NewRequest(http.MethodGet, "/pix/"+payment.EndToEndID, nil)))
	if len(got.Devolucoes) != 1 || got.Devolucoes[0].Status != "DEVOLVIDO" {
		t.Fatalf("devolucoes = %+v", got.Devolucoes)
	}

	delivered := awaitCallback(t, calls)
	if len(delivered.Pix) != 1 || len(delivered.Pix[0].Devolucoes) != 1 {
		t.Fatalf("callback = %s, want the refund included", delivered.raw)
	}

	events, err := st.EventsByAggregate(t.Context(), store.PaymentAggregate(payment.EndToEndID))
	if err != nil {
		t.Fatalf("EventsByAggregate: %v", err)
	}
	var types []string
	for _, e := range events {
		types = append(types, e.Type)
	}
	for _, want := range []string{store.EventPixReceived, store.EventRefundRequested, store.EventRefundSettled} {
		if !slices.Contains(types, want) {
			t.Errorf("event %q missing from %v", want, types)
		}
	}
}

// INV-2: replaying a refund id returns the original and notifies once.
func TestDevolucaoIsIdempotent(t *testing.T) {
	h, st := newServerWith(t, fastWebhooks())
	echo, calls := echoServer(t)

	putJSON(t, h, "/webhook/dev@example.com", fmt.Sprintf(`{"webhookUrl":%q}`, echo.URL))
	payment := payCharge(t, h, validTxID)
	awaitCallback(t, calls)

	first := putJSON(t, h, "/pix/"+payment.EndToEndID+"/devolucao/dev1", `{"valor":"10.00"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d (body: %s)", first.Code, first.Body)
	}
	awaitCallback(t, calls)

	replay := putJSON(t, h, "/pix/"+payment.EndToEndID+"/devolucao/dev1", `{"valor":"10.00"}`)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want 200 (body: %s)", replay.Code, replay.Body)
	}

	var rows int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM refunds`).Scan(&rows); err != nil {
		t.Fatalf("count refunds: %v", err)
	}
	if rows != 1 {
		t.Errorf("refunds rows = %d, want 1", rows)
	}

	select {
	case <-calls:
		t.Error("a replay triggered a second callback")
	case <-time.After(200 * time.Millisecond):
	}
}

// INV-4: a payment cannot give back more than it took.
func TestDevolucaoIsBoundedByTheSettledAmount(t *testing.T) {
	h, _ := newServer(t)

	payment := payCharge(t, h, validTxID)

	rec := putJSON(t, h, "/pix/"+payment.EndToEndID+"/devolucao/dev1", `{"valor":"10.01"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body)
	}
	if got := decodeProblem(t, rec).Type; !strings.HasSuffix(got, "DevolucaoOperacaoInvalida") {
		t.Errorf("problem.type = %q", got)
	}

	// The full amount goes through, and a second id finds nothing left.
	if rec := putJSON(t, h, "/pix/"+payment.EndToEndID+"/devolucao/dev1", `{"valor":"10.00"}`); rec.Code != http.StatusCreated {
		t.Fatalf("full refund: status = %d (body: %s)", rec.Code, rec.Body)
	}
	rec = putJSON(t, h, "/pix/"+payment.EndToEndID+"/devolucao/dev2", `{"valor":"10.00"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("second refund: status = %d, want 400 (body: %s)", rec.Code, rec.Body)
	}
}

func TestDevolucaoValidation(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		body     string
		wantCode int
	}{
		{name: "missing valor", id: "dev1", body: `{}`, wantCode: http.StatusBadRequest},
		{name: "malformed valor", id: "dev1", body: `{"valor":"10"}`, wantCode: http.StatusBadRequest},
		{name: "partial refund", id: "dev1", body: `{"valor":"5.00"}`, wantCode: http.StatusBadRequest},
		{name: "id with punctuation", id: "dev-1", body: `{"valor":"10.00"}`, wantCode: http.StatusBadRequest},
		{name: "id too long", id: strings.Repeat("a", 36), body: `{"valor":"10.00"}`, wantCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newServer(t)
			payment := payCharge(t, h, validTxID)

			rec := putJSON(t, h, "/pix/"+payment.EndToEndID+"/devolucao/"+tt.id, tt.body)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantCode, rec.Body)
			}
		})
	}
}

func TestDevolucaoOfUnknownPayment(t *testing.T) {
	h, _ := newServer(t)

	rec := putJSON(t, h, "/pix/E12345678202607311204x7k2q90000f/devolucao/dev1", `{"valor":"10.00"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body: %s)", rec.Code, rec.Body)
	}
}

// A payee with no webhook is not an error: the payment settles all the same.
func TestPayWithoutAWebhookStillSettles(t *testing.T) {
	h, _ := newServerWith(t, fastWebhooks())

	payment := payCharge(t, h, validTxID)
	if payment.EndToEndID == "" {
		t.Fatal("no payment was created")
	}
}

// A failing endpoint does not fail the payment: the callback is retried, then
// recorded as failed on the payment's timeline.
func TestFailedDeliveryIsRecorded(t *testing.T) {
	h, st := newServerWith(t, fastWebhooks())

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer dead.Close()

	putJSON(t, h, "/webhook/dev@example.com", fmt.Sprintf(`{"webhookUrl":%q}`, dead.URL))
	payment := payCharge(t, h, validTxID)

	waitForEvent(t, st, store.PaymentAggregate(payment.EndToEndID), store.EventWebhookFailed)
}

// waitForEvent polls the log until typ shows up on the aggregate. Webhook
// delivery is asynchronous by design, so a reader has to wait for it.
func waitForEvent(t *testing.T, st *store.Store, aggregate, typ string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		events, err := st.EventsByAggregate(t.Context(), aggregate)
		if err != nil {
			t.Fatalf("EventsByAggregate: %v", err)
		}
		for _, e := range events {
			if e.Type == typ {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("event %q never reached the log of %s", typ, aggregate)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
