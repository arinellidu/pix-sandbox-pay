package console_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arinellidu/pix-sandbox-pay/internal/core"
	"github.com/arinellidu/pix-sandbox-pay/internal/rng"
	"github.com/arinellidu/pix-sandbox-pay/internal/store"
	"github.com/arinellidu/pix-sandbox-pay/web/console"
)

const txid = "abc123def456ghi789jkl012mno345"

// base is relative to now: a charge fixed in the past would read EXPIRADA the
// day after this file was written, and the test would be asserting the wrong
// state for the wrong reason.
var base = time.Now().UTC().Truncate(time.Second)

func newConsole(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "data", "sandbox.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := console.New(st, console.Config{Version: "test", Seed: 42, Log: log})

	// Mounted the way the API mounts it, so the paths under test are the paths
	// a browser actually requests.
	mux := http.NewServeMux()
	mux.Handle("/console/", http.StripPrefix("/console", srv.Router()))
	return mux, st
}

func seedCharge(t *testing.T, st *store.Store, id string, cents int64) core.Charge {
	t.Helper()
	charge := core.Charge{
		TxID:        id,
		Status:      core.StatusAtiva,
		AmountCents: cents,
		Chave:       "dev@example.com",
		Expiracao:   3600,
		EMV:         "00020101021226" + id,
		CreatedAt:   base,
		ExpiresAt:   base.Add(time.Hour),
	}
	stored, _, err := st.CreateCharge(context.Background(), charge)
	if err != nil {
		t.Fatalf("CreateCharge: %v", err)
	}
	return stored
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestLedgerRendersCharges(t *testing.T) {
	h, st := newConsole(t)
	seedCharge(t, st, txid, 1000)

	rec := get(t, h, "/console/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q", ct)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"pix-sandbox",
		"ATIVA",
		"10.00",
		"dev@example.com",
		"/console/cob/" + txid,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("ledger does not mention %q", want)
		}
	}
}

// The direction contract has to survive into the emitted markup, or nothing
// downstream can audit what this surface was built against.
func TestLedgerCarriesTheDirectionContract(t *testing.T) {
	h, _ := newConsole(t)

	body := get(t, h, "/console/").Body.String()
	for _, want := range []string{"THESIS:", "OWN-WORLD:", "FIRST VIEWPORT:", "c7b303d7"} {
		if !strings.Contains(body, want) {
			t.Errorf("emitted markup lost %q", want)
		}
	}
}

func TestLedgerEmptyState(t *testing.T) {
	h, _ := newConsole(t)

	body := get(t, h, "/console/").Body.String()
	if !strings.Contains(body, "No charges yet") {
		t.Error("empty ledger does not say it is empty")
	}
	// The command that ends the emptiness is the point of the state.
	if !strings.Contains(body, "curl -X POST localhost:8080/cob") {
		t.Error("empty ledger does not show how to create a charge")
	}
}

// The poll marks what arrived after the instant the viewer last saw, which is
// what the arrival animation hangs on.
func TestRowsMarkWhatArrivedSince(t *testing.T) {
	h, st := newConsole(t)
	seedCharge(t, st, txid, 1000)

	fresh := get(t, h, "/console/rows?since="+base.Add(-time.Hour).Format(time.RFC3339Nano))
	if !strings.Contains(fresh.Body.String(), "row--fresh") {
		t.Error("a charge created after `since` was not marked fresh")
	}

	stale := get(t, h, "/console/rows?since="+base.Add(time.Hour).Format(time.RFC3339Nano))
	if strings.Contains(stale.Body.String(), "row--fresh") {
		t.Error("a charge created before `since` was marked fresh")
	}

	// The fragment carries the next poll's watermark, so no viewer state is
	// kept on the server.
	if !strings.Contains(fresh.Body.String(), "/console/rows?since=") {
		t.Error("the fragment does not carry the next poll URL")
	}
}

func TestChargePageShowsTheRecordedTimeline(t *testing.T) {
	h, st := newConsole(t)
	ctx := context.Background()
	seedCharge(t, st, txid, 1000)

	src := rng.New(rng.DefaultSeed)
	if _, err := st.SettleCharge(ctx, txid, "Coffee", base.Add(time.Minute),
		func(seq int64) (string, error) { return core.NewE2EID(src, base, seq) },
	); err != nil {
		t.Fatalf("SettleCharge: %v", err)
	}

	rec := get(t, h, "/console/cob/"+txid)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	for _, want := range []string{
		txid,
		"cob.created",
		"Recorded transitions",
		"BR Code",
		"log id",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("charge page does not mention %q", want)
		}
	}
	// The payload speaks the domain's language: cents, not decimal strings.
	if !strings.Contains(body, "amount_cents") {
		t.Error("the event payload is not rendered")
	}
}

func TestChargePageNotFound(t *testing.T) {
	h, _ := newConsole(t)

	rec := get(t, h, "/console/cob/"+txid)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No charge with this txid") {
		t.Error("the 404 page does not say what is missing")
	}
	// A miss stays inside the console's own world rather than falling back to
	// the browser's default error page.
	if !strings.Contains(rec.Body.String(), "pix-sandbox") {
		t.Error("the 404 page lost the layout")
	}
}

// Assets ship inside the binary: the console must render with no network.
func TestStaticAssetsAreEmbedded(t *testing.T) {
	h, _ := newConsole(t)

	for _, asset := range []string{
		"/console/static/console.css",
		"/console/static/console.js",
		"/console/static/htmx.min.js",
		"/console/static/favicon.svg",
		"/console/static/fonts/archivo-latin.woff2",
		"/console/static/fonts/fragment-mono-latin.woff2",
	} {
		rec := get(t, h, asset)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want 200", asset, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("GET %s: empty body", asset)
		}
	}
}

// Asset URLs are fingerprinted, so `immutable` cannot serve one release's
// stylesheet to the next release's markup.
func TestAssetURLsAreVersioned(t *testing.T) {
	h, _ := newConsole(t)

	body := get(t, h, "/console/").Body.String()
	if !strings.Contains(body, "/console/static/console.css?v=") {
		t.Error("the stylesheet URL carries no version")
	}
	if !strings.Contains(body, "/console/static/htmx.min.js?v=") {
		t.Error("the htmx URL carries no version")
	}

	rec := get(t, h, "/console/static/console.css")
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("cache-control = %q, want immutable", cc)
	}
}

// Nothing external: no CDN, no font host, no analytics.
func TestNoExternalRequests(t *testing.T) {
	h, st := newConsole(t)
	seedCharge(t, st, txid, 1000)

	for _, path := range []string{"/console/", "/console/cob/" + txid, "/console/static/console.css"} {
		body := get(t, h, path).Body.String()
		for _, forbidden := range []string{"http://", "https://", "//cdn", "//unpkg", "fonts.g"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s reaches outside the binary: found %q", path, forbidden)
			}
		}
	}
}

// A store that cannot be read is the moment the reader most needs to know
// which surface broke — so the failure stays inside the console's own world
// rather than falling back to a plain-text browser default.
func TestStoreFailureRendersInsideTheConsole(t *testing.T) {
	h, st := newConsole(t)
	st.Close()

	rec := get(t, h, "/console/")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	body := rec.Body.String()
	for _, want := range []string{"pix-sandbox", "The console could not", "console.css"} {
		if !strings.Contains(body, want) {
			t.Errorf("the error page lost %q", want)
		}
	}
}

// A removal status is thirty-one characters; the ledger prints a label its
// column can hold and keeps the enum itself on the row.
func TestRemovedStatusKeepsItsEnumOnTheRow(t *testing.T) {
	h, st := newConsole(t)
	seedCharge(t, st, txid, 1000)

	if _, err := st.DB().Exec(
		`UPDATE charges SET status = ? WHERE txid = ?`,
		string(core.StatusRemovidaPeloUsuarioRecebedor), txid,
	); err != nil {
		t.Fatalf("update status: %v", err)
	}

	body := get(t, h, "/console/").Body.String()
	if !strings.Contains(body, `title="REMOVIDA_PELO_USUARIO_RECEBEDOR"`) {
		t.Error("the row does not carry the full status")
	}
	if !strings.Contains(body, ">REMOVIDA</span>") {
		t.Error("the column does not print the short label")
	}
}

// The console watches; it never acts.
func TestConsoleIsReadOnly(t *testing.T) {
	h, _ := newConsole(t)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/console/", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /console/: status = %d, want 405", method, rec.Code)
		}
	}
}
