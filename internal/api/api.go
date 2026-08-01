// Package api exposes the HTTP surface: the BACEN-compatible API Pix
// endpoints plus the sandbox-only controls. S0 ships /health and the mock
// OAuth2 token endpoint; the rest lands in later phases.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/arinelliquebec/pix-sandbox/internal/rng"
	"github.com/arinelliquebec/pix-sandbox/internal/store"
)

// Server holds the dependencies shared by the handlers.
type Server struct {
	store *store.Store
	rng   *rng.Source
	log   *slog.Logger
}

// New builds a Server.
func New(st *store.Store, src *rng.Source, log *slog.Logger) *Server {
	return &Server{store: st, rng: src, log: log}
}

// Router returns the fully wired HTTP handler.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/health", s.handleHealth)
	r.Post("/oauth/token", s.handleToken)

	return r
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Headers are already out; nothing left to do but drop the response.
		return
	}
}
