package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arinelliquebec/pix-sandbox/internal/store"
)

type webhookRegistration struct {
	WebhookURL string `json:"webhookUrl"`
	Chave      string `json:"chave"`
	Criacao    string `json:"criacao"`
}

func decodeWebhook(t *testing.T, rec *httptest.ResponseRecorder) webhookRegistration {
	t.Helper()
	var body webhookRegistration
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode webhook: %v", err)
	}
	return body
}

func TestPutAndGetWebhook(t *testing.T) {
	h, st := newServer(t)

	rec := putJSON(t, h, "/webhook/dev@example.com", `{"webhookUrl":"https://example.com/pix"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rec.Code, rec.Body)
	}

	registered := decodeWebhook(t, rec)
	if registered.Chave != "dev@example.com" || registered.WebhookURL != "https://example.com/pix" {
		t.Errorf("registration = %+v", registered)
	}
	if registered.Criacao == "" {
		t.Error("criacao is empty")
	}

	rec = do(t, h, httptest.NewRequest(http.MethodGet, "/webhook/dev@example.com", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}
	if got := decodeWebhook(t, rec); got != registered {
		t.Errorf("GET returned %+v, want %+v", got, registered)
	}

	events, err := st.EventsByAggregate(t.Context(), store.WebhookAggregate("dev@example.com"))
	if err != nil {
		t.Fatalf("EventsByAggregate: %v", err)
	}
	if len(events) != 1 || events[0].Type != store.EventWebhookRegistered {
		t.Errorf("events = %+v, want one webhook.registered", events)
	}
}

// Re-registering replaces the URL and keeps the original `criacao`, which the
// specification defines as the registration's creation instant.
func TestPutWebhookReplaces(t *testing.T) {
	h, _ := newServer(t)

	first := decodeWebhook(t, putJSON(t, h, "/webhook/dev@example.com",
		`{"webhookUrl":"https://example.com/pix"}`))

	rec := putJSON(t, h, "/webhook/dev@example.com", `{"webhookUrl":"https://example.com/other"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 on a replacement (body: %s)", rec.Code, rec.Body)
	}

	replaced := decodeWebhook(t, rec)
	if replaced.WebhookURL != "https://example.com/other" {
		t.Errorf("webhookUrl = %q, want the replacement", replaced.WebhookURL)
	}
	if replaced.Criacao != first.Criacao {
		t.Errorf("criacao = %q, want the original %q", replaced.Criacao, first.Criacao)
	}
}

func TestPutWebhookValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing url", body: `{}`},
		{name: "not a url", body: `{"webhookUrl":"nope"}`},
		{name: "wrong scheme", body: `{"webhookUrl":"ftp://example.com/pix"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newServer(t)

			rec := putJSON(t, h, "/webhook/dev@example.com", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body)
			}
			if got := decodeProblem(t, rec).Type; !strings.HasSuffix(got, "WebhookOperacaoInvalida") {
				t.Errorf("problem.type = %q", got)
			}
		})
	}
}

// http is accepted where BACEN demands https: the endpoint under test is
// usually an echo server on localhost.
func TestPutWebhookAcceptsLocalHTTP(t *testing.T) {
	h, _ := newServer(t)

	rec := putJSON(t, h, "/webhook/dev@example.com", `{"webhookUrl":"http://127.0.0.1:9999/hook"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rec.Code, rec.Body)
	}
}

func TestGetWebhookNotRegistered(t *testing.T) {
	h, _ := newServer(t)

	rec := do(t, h, httptest.NewRequest(http.MethodGet, "/webhook/nobody@example.com", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got := decodeProblem(t, rec).Type; !strings.HasSuffix(got, "WebhookNaoEncontrado") {
		t.Errorf("problem.type = %q", got)
	}
}
