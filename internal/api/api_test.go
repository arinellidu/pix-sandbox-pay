package api_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arinellidu/pix-sandbox-pay/internal/api"
	"github.com/arinellidu/pix-sandbox-pay/internal/rng"
	"github.com/arinellidu/pix-sandbox-pay/internal/store"
)

func newServer(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	return newServerWith(t, api.Config{})
}

// newServerAt builds a server whose clock the test controls. Passing a nil
// clock leaves it at time.Now.
func newServerAt(t *testing.T, now func() time.Time) (http.Handler, *store.Store) {
	t.Helper()
	return newServerWith(t, api.Config{Now: now})
}

// newServerWith builds a server on a fresh store and drains its webhook
// dispatcher when the test ends, so no delivery outlives the test that
// triggered it.
func newServerWith(t *testing.T, cfg api.Config) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "data", "sandbox.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	return newServerOn(t, st, cfg), st
}

// newServerOn builds a server over an existing store — a fresh rng on an old
// database is exactly what a process restart looks like.
func newServerOn(t *testing.T, st *store.Store, cfg api.Config) http.Handler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := api.New(st, rng.New(rng.DefaultSeed), log, cfg)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Close(ctx); err != nil {
			t.Errorf("close server: %v", err)
		}
	})
	return srv.Router()
}

func do(t *testing.T, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	h, _ := newServer(t)

	rec := do(t, h, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	// A CI run has to be able to name the build that answered it.
	if body.Version == "" {
		t.Error("health reports no version")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}
}

func TestTokenFormEncoded(t *testing.T) {
	h, st := newServer(t)

	form := url.Values{
		"grant_type": {"client_credentials"},
		"client_id":  {"demo-psp"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := do(t, h, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}

	var body struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", body.TokenType)
	}
	if !strings.HasPrefix(body.AccessToken, "sandbox_") || len(body.AccessToken) <= len("sandbox_") {
		t.Errorf("access_token = %q, want non-empty sandbox_ token", body.AccessToken)
	}
	if body.ExpiresIn != 3600 {
		t.Errorf("expires_in = %d, want 3600", body.ExpiresIn)
	}
	if body.Scope == "" {
		t.Error("scope is empty")
	}

	events, err := st.EventsByAggregate(t.Context(), "oauth")
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 1 || events[0].Type != "oauth.token.issued" {
		t.Fatalf("events = %+v, want one oauth.token.issued", events)
	}
}

func TestTokenJSONBodyAndCustomScope(t *testing.T) {
	h, _ := newServer(t)

	req := httptest.NewRequest(http.MethodPost, "/oauth/token",
		strings.NewReader(`{"grant_type":"client_credentials","scope":"cob.write"}`))
	req.Header.Set("Content-Type", "application/json")

	rec := do(t, h, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}

	var body struct {
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Scope != "cob.write" {
		t.Errorf("scope = %q, want cob.write", body.Scope)
	}
}

// `curl -X POST localhost:8080/oauth/token` with no body must still work: the demo loop
// should not require the caller to spell out the only grant there is.
func TestTokenEmptyBody(t *testing.T) {
	h, _ := newServer(t)

	rec := do(t, h, httptest.NewRequest(http.MethodPost, "/oauth/token", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}
}

func TestTokenRejectsOtherGrants(t *testing.T) {
	h, _ := newServer(t)

	form := url.Values{"grant_type": {"authorization_code"}}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := do(t, h, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "unsupported_grant_type" {
		t.Errorf("error = %q, want unsupported_grant_type", body.Error)
	}
}

func TestTokenRejectsMalformedJSON(t *testing.T) {
	h, _ := newServer(t)

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")

	rec := do(t, h, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	h, _ := newServer(t)

	rec := do(t, h, httptest.NewRequest(http.MethodGet, "/cob/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
