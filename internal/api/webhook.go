package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/arinelliquebec/pix-sandbox/internal/core"
	"github.com/arinelliquebec/pix-sandbox/internal/store"
)

type webhookRequest struct {
	WebhookURL string `json:"webhookUrl"`
}

type webhookResponse struct {
	WebhookURL string `json:"webhookUrl"`
	Chave      string `json:"chave"`
	Criacao    string `json:"criacao"`
}

// handlePutWebhook serves PUT /webhook/{chave}.
//
// BACEN answers an empty 2xx here; the sandbox returns the stored registration
// instead — strictly more than a client needs, and it makes the demo loop
// legible on a terminal. 201 marks a new registration, 200 a replacement.
func (s *Server) handlePutWebhook(w http.ResponseWriter, r *http.Request) {
	chave := chi.URLParam(r, "chave")
	if err := core.ValidateChave(chave); err != nil {
		badWebhook(w, "the webhook could not be registered",
			[]violacao{{Razao: err.Error(), Propriedade: "chave"}})
		return
	}

	var req webhookRequest
	if err := decodeJSON(r, &req); err != nil {
		malformedRequest(w, err.Error())
		return
	}
	if err := core.ValidateWebhookURL(req.WebhookURL); err != nil {
		badWebhook(w, "the webhook could not be registered",
			[]violacao{{Razao: err.Error(), Propriedade: "webhookUrl"}})
		return
	}

	hook, created, err := s.store.PutWebhook(r.Context(), chave, req.WebhookURL, s.now())
	if err != nil {
		s.log.Error("register webhook", "chave", chave, "err", err)
		internalError(w)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, webhookDTO(hook))
}

// handleGetWebhook serves GET /webhook/{chave}.
func (s *Server) handleGetWebhook(w http.ResponseWriter, r *http.Request) {
	chave := chi.URLParam(r, "chave")

	hook, err := s.store.GetWebhook(r.Context(), chave)
	switch {
	case errors.Is(err, store.ErrNotFound):
		notFoundWebhook(w, "no webhook registered for "+chave)
		return
	case err != nil:
		s.log.Error("load webhook", "chave", chave, "err", err)
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, webhookDTO(hook))
}

func webhookDTO(h core.Webhook) webhookResponse {
	return webhookResponse{
		WebhookURL: h.URL,
		Chave:      h.Chave,
		Criacao:    h.CreatedAt.UTC().Format(bacenTime),
	}
}
