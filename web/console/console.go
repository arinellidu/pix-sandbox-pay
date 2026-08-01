// Package console serves the embedded read-only UI: the ledger of charges and
// the recorded timeline of any one of them.
//
// It reads the same projections and the same append-only log the API writes;
// it is a second reader, never a second source of truth. Nothing here mutates
// anything — the console watches, the terminal acts.
package console

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/arinellidu/pix-sandbox-pay/internal/core"
	"github.com/arinellidu/pix-sandbox-pay/internal/store"
)

//go:embed static
var staticFS embed.FS

// assetVersion fingerprints the embedded assets so their URLs change whenever
// the binary's copy does. Without it, `immutable` would mean a browser that
// saw one release keeps its stylesheet across every upgrade.
var assetVersion = fingerprint()

func fingerprint() string {
	h := fnv.New64a()
	err := fs.WalkDir(staticFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := staticFS.ReadFile(path)
		if err != nil {
			return err
		}
		h.Write([]byte(path))
		h.Write(b)
		return nil
	})
	if err != nil {
		// Unreachable with an embedded FS; a changing value is the safe answer
		// anyway, since it only costs a cache miss.
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return strconv.FormatUint(h.Sum64(), 36)
}

// asset builds a versioned URL for an embedded file.
func asset(name string) templ.SafeURL {
	return templ.SafeURL("/console/static/" + name + "?v=" + assetVersion)
}

// DefaultLimit is how many charges the ledger shows.
const DefaultLimit = 60

// pollTrigger is how often the ledger asks for new rows. Two seconds is slower
// than a human alt-tabs back from the terminal and cheap enough to run against
// SQLite indefinitely.
const pollTrigger = "every 2s"

// createCommand is the one call that ends an empty ledger. It lives here
// rather than in the template because the payload's braces are templ syntax.
const createCommand = `curl -X POST localhost:8080/cob -H 'Content-Type: application/json' \
     -d '{"valor":{"original":"10.00"},"chave":"dev@example.com"}'`

// Config carries what the console shows about the run itself.
type Config struct {
	Version string
	Seed    uint64
	Log     *slog.Logger
}

// Server is the console's HTTP surface.
type Server struct {
	store *store.Store
	cfg   Config
	log   *slog.Logger
}

// New builds the console.
func New(st *store.Store, cfg Config) *Server {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Server{store: st, cfg: cfg, log: log}
}

// Router returns the console's handler, to be mounted under /console.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/", s.handleLedger)
	r.Get("/rows", s.handleRows)
	r.Get("/cob/{txid}", s.handleCharge)
	// Assets ship inside the binary: the console must work with no network.
	r.Handle("/static/*", staticHandler())
	return r
}

// staticHandler serves the embedded assets from the route's own wildcard
// rather than from the request path, so the console keeps working whichever
// way it is mounted — chi's Mount leaves the prefix on r.URL.Path, a
// StripPrefix mount does not, and a handler that assumes either is a 404
// waiting for the next caller.
func staticHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean("/" + chi.URLParam(r, "*"))
		if strings.Contains(name, "..") {
			http.NotFound(w, r)
			return
		}
		// The URLs carry a content fingerprint, so a long cache is safe and
		// keeps the poll cheap.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFileFS(w, r, staticFS, "static"+name)
	})
}

// railView is the sheet's left margin: what is true about this run.
type railView struct {
	Version string
	Seed    string
	// Polling is set on the surface that actually polls, so the live/lost
	// indicator never claims a page is refreshing when it is not.
	Polling   bool
	HasCounts bool
	Charges   string
	Payments  string
	Refunds   string
	Webhooks  string
	Events    string
}

// LedgerView is everything the index renders.
type LedgerView struct {
	Rail railView
	Rows RowsView
}

// RowsView is the polled fragment: the ledger's body plus the watermark the
// next poll carries back, which is how a row knows it has just arrived.
type RowsView struct {
	Charges []ChargeRow
	Since   string
	PollURL string
	Poll    string
}

// ChargeRow is one module of the ledger.
type ChargeRow struct {
	TxID  string
	Short string
	// Status is what the column can print; StatusFull is the enum itself,
	// which the removal states are too long to show in a scanning surface.
	Status      string
	StatusFull  string
	StatusClass string
	Amount      string
	Chave       string
	Created     string
	Full        string
	// Fresh marks a charge this viewer had not seen on the previous poll.
	Fresh bool
}

// ChargeView is the detail page: one charge, everything recorded about it.
type ChargeView struct {
	Rail        railView
	TxID        string
	Status      string
	StatusClass string
	Amount      string
	Chave       string
	Created     string
	Expires     string
	// Solicitacao is the payee's message to the payer, when there was one.
	Solicitacao string
	Devedor     string
	EMV         string
	Payment     *PaymentView
	Events      []EventView
}

// PaymentView is the settled pix, when the charge has one.
type PaymentView struct {
	E2EID    string
	Horario  string
	Amount   string
	Refunded string
	Status   string
	Info     string
}

// EventView is one module of the timeline.
type EventView struct {
	Seq  string
	Type string
	At   string
	Full string
	// DayBreak is set on the first event of each calendar day, so a timeline
	// printing times alone cannot make an expiry a day later look simultaneous.
	DayBreak string
	Pairs    []Pair
	// IndentClass steps the module across the grid: a charge event sits at the
	// margin, the payment it settled into one step in, the callback that
	// announced that payment two. The rule is the reading.
	IndentClass string
	// AccentClass prints the kind of transition as a rule at the module's edge.
	AccentClass string
}

// Pair is one field of an event payload. Block marks a value that is itself
// structured: it takes the full width rather than being squeezed into a column
// it will overflow.
type Pair struct {
	Key   string
	Value string
	Block bool
}

func (s *Server) handleLedger(w http.ResponseWriter, r *http.Request) {
	rows, err := s.rows(r.Context(), "")
	if err != nil {
		s.fail(w, r, "list charges", err)
		return
	}
	stats, err := s.store.Stats(r.Context())
	if err != nil {
		s.fail(w, r, "read stats", err)
		return
	}

	rail := s.rail(&stats)
	rail.Polling = true
	s.render(w, r, ledger(LedgerView{Rail: rail, Rows: rows}))
}

// handleRows serves the poll. `since` is the newest creation instant this
// viewer has already seen, so the server can mark what arrived after it
// without holding any per-viewer state.
func (s *Server) handleRows(w http.ResponseWriter, r *http.Request) {
	rows, err := s.rows(r.Context(), r.URL.Query().Get("since"))
	if err != nil {
		s.fail(w, r, "list charges", err)
		return
	}
	s.render(w, r, ledgerRows(rows))
}

func (s *Server) rows(ctx context.Context, since string) (RowsView, error) {
	charges, err := s.store.ListCharges(ctx, DefaultLimit)
	if err != nil {
		return RowsView{}, err
	}

	now := time.Now().UTC()
	view := RowsView{Since: since, Poll: pollTrigger}
	for _, c := range charges {
		created := c.CreatedAt.UTC()
		stamp := created.Format(time.RFC3339Nano)
		status := string(c.EffectiveStatus(now))
		view.Charges = append(view.Charges, ChargeRow{
			TxID:        c.TxID,
			Short:       shorten(c.TxID),
			Status:      statusLabel(status),
			StatusFull:  status,
			StatusClass: statusClass(status),
			Amount:      core.FormatAmount(c.AmountCents),
			Chave:       c.Chave,
			Created:     created.Format("2006-01-02 15:04:05"),
			Full:        stamp,
			Fresh:       since != "" && stamp > since,
		})
		if stamp > view.Since {
			view.Since = stamp
		}
	}

	view.PollURL = "/console/rows?" + url.Values{"since": {view.Since}}.Encode()
	return view, nil
}

func (s *Server) handleCharge(w http.ResponseWriter, r *http.Request) {
	txid := chi.URLParam(r, "txid")

	// The rail carries the run's counts on every page: the margin is where a
	// reader checks that the emulator as a whole is where they think it is.
	stats, err := s.store.Stats(r.Context())
	if err != nil {
		s.fail(w, r, "read stats", err)
		return
	}
	rail := s.rail(&stats)

	charge, err := s.store.GetCharge(r.Context(), txid)
	switch {
	case errors.Is(err, store.ErrNotFound):
		w.WriteHeader(http.StatusNotFound)
		s.render(w, r, notFoundPage(txid, rail))
		return
	case err != nil:
		s.fail(w, r, "read charge", err)
		return
	}

	events, err := s.store.ChargeTimeline(r.Context(), txid)
	if err != nil {
		s.fail(w, r, "read timeline", err)
		return
	}

	status := string(charge.EffectiveStatus(time.Now().UTC()))
	view := ChargeView{
		Rail:        rail,
		TxID:        charge.TxID,
		Status:      status,
		StatusClass: statusClass(status),
		Amount:      core.FormatAmount(charge.AmountCents),
		Chave:       charge.Chave,
		Created:     charge.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
		Expires:     charge.ExpiresAt.UTC().Format("2006-01-02 15:04:05"),
		Solicitacao: charge.SolicitacaoPagador,
		EMV:         charge.EMV,
	}
	if charge.Devedor != nil {
		view.Devedor = devedorLine(*charge.Devedor)
	}

	payment, err := s.store.PaymentByTxID(r.Context(), txid)
	switch {
	case err == nil:
		view.Payment = &PaymentView{
			E2EID:    payment.E2EID,
			Horario:  payment.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
			Amount:   core.FormatAmount(payment.AmountCents),
			Refunded: core.FormatAmount(payment.RefundedCents),
			Status:   string(payment.Status),
			Info:     payment.InfoPagador,
		}
	case !errors.Is(err, store.ErrNotFound):
		s.fail(w, r, "read payment", err)
		return
	}

	var previousDay string
	for _, e := range events {
		ev := eventView(e)
		if day := e.CreatedAt.UTC().Format("2006-01-02"); day != previousDay {
			ev.DayBreak = day
			previousDay = day
		}
		view.Events = append(view.Events, ev)
	}

	s.render(w, r, chargePage(view))
}

func (s *Server) rail(stats *store.Stats) railView {
	rail := railView{Version: s.version(), Seed: strconv.FormatUint(s.cfg.Seed, 10)}
	if stats != nil {
		rail.HasCounts = true
		rail.Charges = strconv.FormatInt(stats.Charges, 10)
		rail.Payments = strconv.FormatInt(stats.Payments, 10)
		rail.Refunds = strconv.FormatInt(stats.Refunds, 10)
		rail.Webhooks = strconv.FormatInt(stats.Webhooks, 10)
		rail.Events = strconv.FormatInt(stats.Events, 10)
	}
	return rail
}

func eventView(e store.Event) EventView {
	v := EventView{
		Seq:   strconv.FormatInt(e.ID, 10),
		Type:  e.Type,
		At:    e.CreatedAt.UTC().Format("15:04:05.000"),
		Full:  e.CreatedAt.UTC().Format(time.RFC3339Nano),
		Pairs: payloadPairs(e.Payload),
	}

	switch {
	case strings.HasPrefix(e.Type, "webhook."):
		v.IndentClass = "event--step-2"
	case strings.HasPrefix(e.Aggregate, "pix:"):
		v.IndentClass = "event--step-1"
	}

	switch e.Type {
	case store.EventPixReceived, store.EventChargeSettled, store.EventWebhookDelivered:
		v.AccentClass = "event--settled"
	case store.EventChargeExpired, store.EventWebhookFailed:
		v.AccentClass = "event--failed"
	case store.EventRefundRequested, store.EventRefundSettled:
		v.AccentClass = "event--refund"
	}
	return v
}

// statusLabel is what the ledger's column can actually print.
// REMOVIDA_PELO_USUARIO_RECEBEDOR is thirty-one characters; a column wide
// enough for it would be wider than the amount and the key together. The enum
// is not abbreviated away — it stays on the row's title and on the charge page.
func statusLabel(status string) string {
	if strings.HasPrefix(status, "REMOVIDA_") {
		return "REMOVIDA"
	}
	return status
}

func statusClass(status string) string {
	switch {
	case status == string(core.StatusAtiva):
		return "status--ativa"
	case status == string(core.StatusConcluida):
		return "status--concluida"
	case status == string(core.StatusExpirada):
		return "status--expirada"
	default:
		return "status--removida"
	}
}

// payloadPairs flattens an event payload into ordered fields. Nested objects
// keep their JSON: the shape of the log is part of what the console shows.
func payloadPairs(raw json.RawMessage) []Pair {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return []Pair{{Key: "payload", Value: string(raw), Block: true}}
	}

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]Pair, 0, len(keys))
	for _, k := range keys {
		value, block := formatValue(fields[k])
		pairs = append(pairs, Pair{Key: k, Value: value, Block: block})
	}
	// Structured values take the full width, so they come last: a two-column
	// grid reads worse when a block interrupts it mid-row.
	sort.SliceStable(pairs, func(i, j int) bool { return !pairs[i].Block && pairs[j].Block })
	return pairs
}

// formatValue renders one payload value, reporting whether it is structured
// and therefore needs the full row.
func formatValue(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, false
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10), false
		}
		return strconv.FormatFloat(t, 'f', -1, 64), false
	case bool:
		return strconv.FormatBool(t), false
	case nil:
		return "null", false
	default:
		// Compact, not indented: one line of JSON in its own row costs three
		// lines less than a pretty-printed block that dwarfs its own event.
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t), true
		}
		return string(b), true
	}
}

func devedorLine(d core.Devedor) string {
	switch {
	case d.CPF != "":
		return d.Nome + " · CPF " + d.CPF
	case d.CNPJ != "":
		return d.Nome + " · CNPJ " + d.CNPJ
	default:
		return d.Nome
	}
}

// shorten keeps a txid scannable in a column without pretending it is short:
// the head is what a reader matches against what their terminal printed.
func shorten(txid string) string {
	if len(txid) <= 14 {
		return txid
	}
	return txid[:10] + "…" + txid[len(txid)-4:]
}

func (s *Server) version() string {
	if s.cfg.Version == "" {
		return "dev"
	}
	return s.cfg.Version
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		s.log.Error("render console", "err", err)
	}
}

// fail answers inside the console's own world. A plain http.Error would drop
// the reader onto a white browser default page at the exact moment they most
// need to know which surface is broken.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	s.log.Error("console: "+what, "err", err)
	w.WriteHeader(http.StatusInternalServerError)
	s.render(w, r, errorPage(what, s.rail(nil)))
}
