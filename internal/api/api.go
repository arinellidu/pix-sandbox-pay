// Package api exposes the HTTP surface: the BACEN-compatible API Pix
// endpoints plus the sandbox-only controls. S1 ships the charge (`cob`)
// resource and its BR Code; payments and webhooks land next.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/arinelliquebec/pix-sandbox/internal/emv"
	"github.com/arinelliquebec/pix-sandbox/internal/rng"
	"github.com/arinelliquebec/pix-sandbox/internal/store"
)

// DefaultBaseURL is the host a charge's location points at when none is set.
const DefaultBaseURL = "localhost:8080"

// Config carries what the handlers need to know about the world outside them.
type Config struct {
	// BaseURL is the scheme-less host used to build `loc.location`.
	BaseURL string
	// MerchantName and MerchantCity fill fields 59 and 60 of the BR Code.
	MerchantName string
	MerchantCity string
	// Now supplies the current time. Left nil it is time.Now; the virtual
	// clock will replace it wholesale in a later phase.
	Now func() time.Time
}

func (c Config) withDefaults() Config {
	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	if c.MerchantName == "" {
		c.MerchantName = emv.DefaultMerchantName
	}
	if c.MerchantCity == "" {
		c.MerchantCity = emv.DefaultMerchantCity
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// Server holds the dependencies shared by the handlers.
type Server struct {
	store *store.Store
	rng   *rng.Source
	log   *slog.Logger
	cfg   Config
}

// New builds a Server.
func New(st *store.Store, src *rng.Source, log *slog.Logger, cfg Config) *Server {
	return &Server{store: st, rng: src, log: log, cfg: cfg.withDefaults()}
}

// now returns the current instant in UTC.
func (s *Server) now() time.Time { return s.cfg.Now().UTC() }

// Router returns the fully wired HTTP handler.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/health", s.handleHealth)
	r.Post("/oauth/token", s.handleToken)

	// Registered flat rather than under a chi.Route subrouter so that POST
	// /cob matches without a trailing slash.
	r.Post("/cob", s.handleCreateCob)
	r.Put("/cob/{txid}", s.handleCreateCob)
	r.Get("/cob/{txid}", s.handleGetCob)
	r.Get("/cob/{txid}/qrcode", s.handleGetCobQRCode)

	return r
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encodeJSON(w, body)
}

func encodeJSON(w http.ResponseWriter, body any) {
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Headers are already out; nothing left to do but drop the response.
		return
	}
}
