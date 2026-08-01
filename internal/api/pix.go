package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/arinellidu/pix-sandbox-pay/internal/core"
	"github.com/arinellidu/pix-sandbox-pay/internal/store"
)

// pixResponse is the `Pix` resource of the API Pix: a payment as its payee
// sees it.
type pixResponse struct {
	EndToEndID  string              `json:"endToEndId"`
	TxID        string              `json:"txid"`
	Valor       string              `json:"valor"`
	Chave       string              `json:"chave"`
	Horario     string              `json:"horario"`
	InfoPagador string              `json:"infoPagador,omitempty"`
	Devolucoes  []devolucaoResponse `json:"devolucoes,omitempty"`
}

type horarioDTO struct {
	Solicitacao string `json:"solicitacao"`
	Liquidacao  string `json:"liquidacao,omitempty"`
}

type devolucaoResponse struct {
	ID      string     `json:"id"`
	RtrID   string     `json:"rtrId"`
	Valor   string     `json:"valor"`
	Horario horarioDTO `json:"horario"`
	Status  string     `json:"status"`
	Motivo  string     `json:"motivo,omitempty"`
}

// webhookPayload is the body posted to the payee's endpoint. BACEN sends a
// batch of pix per callback; the sandbox always sends exactly one, so a test
// can assert on it without unpacking a batch that never has more than one
// element.
type webhookPayload struct {
	Pix []pixResponse `json:"pix"`
}

type payRequest struct {
	TxID        string `json:"txid"`
	InfoPagador string `json:"infoPagador"`
}

// handleSandboxPay serves POST /sandbox/pay: the payer's half of the loop. No
// BACEN endpoint covers it because in production it happens at the payer's
// PSP — which is precisely what a local emulator has to stand in for.
//
// It settles the charge, mints the e2eId and notifies the payee.
func (s *Server) handleSandboxPay(w http.ResponseWriter, r *http.Request) {
	var req payRequest
	if err := decodeJSON(r, &req); err != nil {
		bodyError(w, err)
		return
	}

	var violacoes []violacao
	if err := core.ValidateTxID(req.TxID); err != nil {
		violacoes = append(violacoes, violacao{Razao: err.Error(), Propriedade: "txid"})
	}
	if len(req.InfoPagador) > core.MaxInfoPagadorLen {
		violacoes = append(violacoes, violacao{
			Razao:       fmt.Sprintf("infoPagador must be at most %d characters", core.MaxInfoPagadorLen),
			Propriedade: "infoPagador",
		})
	}
	if len(violacoes) > 0 {
		badRequest(w, "the payment could not be simulated", violacoes)
		return
	}

	now := s.now()
	// Settling a pending expiry first keeps the refusal honest: a charge past
	// its window becomes EXPIRADA in the log before it is reported unpayable.
	charge, err := s.store.ExpireCharge(r.Context(), req.TxID, now)
	switch {
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "no charge with txid "+req.TxID)
		return
	case err != nil:
		s.log.Error("load charge", "txid", req.TxID, "err", err)
		internalError(w)
		return
	}

	payment, err := s.store.SettleCharge(r.Context(), req.TxID, req.InfoPagador, now,
		func(seq int64) (string, error) { return core.NewE2EID(s.rng, now, seq) })
	switch {
	case errors.Is(err, store.ErrChargeSettled):
		conflict(w, fmt.Sprintf("charge %s was already settled by %s", req.TxID, payment.E2EID))
		return
	case errors.Is(err, store.ErrChargeNotPayable):
		badRequest(w, fmt.Sprintf("charge %s is %s and cannot be paid",
			req.TxID, charge.EffectiveStatus(now)), nil)
		return
	case err != nil:
		s.log.Error("settle charge", "txid", req.TxID, "err", err)
		internalError(w)
		return
	}

	s.log.Info("pix received", "e2eid", payment.E2EID, "txid", payment.TxID,
		"amount_cents", payment.AmountCents)
	// The settle is committed; the callback it owes must not die with the
	// request. A payer disconnecting — or chi's timeout firing — right after
	// the commit would otherwise drop the delivery without a trace in the log.
	nctx, cancel := notifyContext(r)
	defer cancel()
	s.notify(nctx, payment, nil)
	writeJSON(w, http.StatusCreated, pixDTO(payment, nil))
}

// handleGetPix serves GET /pix/{e2eid}.
func (s *Server) handleGetPix(w http.ResponseWriter, r *http.Request) {
	payment, refunds, ok := s.loadPayment(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, pixDTO(payment, refunds))
}

type devolucaoRequest struct {
	Valor string `json:"valor"`
}

// handleCreateDevolucao serves PUT /pix/{e2eid}/devolucao/{id}.
//
// It is idempotent on the payee-chosen id (INV-2), and bounded by what settled
// (INV-4). Only full refunds are accepted in this phase: partial ones need the
// `componentesValor` breakdown, which arrives with the settlement engine.
func (s *Server) handleCreateDevolucao(w http.ResponseWriter, r *http.Request) {
	e2eID := chi.URLParam(r, "e2eid")
	if err := core.ValidateE2EID(e2eID); err != nil {
		notFoundPix(w, err.Error())
		return
	}

	id := chi.URLParam(r, "id")
	if err := core.ValidateRefundID(id); err != nil {
		badRefund(w, "the refund could not be requested",
			[]violacao{{Razao: err.Error(), Propriedade: "id"}})
		return
	}

	var req devolucaoRequest
	if err := decodeJSON(r, &req); err != nil {
		bodyError(w, err)
		return
	}
	if req.Valor == "" {
		badRefund(w, "the refund could not be requested",
			[]violacao{{Razao: "valor is required", Propriedade: "valor"}})
		return
	}
	amountCents, err := core.ParseAmount(req.Valor)
	if err != nil {
		badRefund(w, "the refund could not be requested",
			[]violacao{{Razao: err.Error(), Propriedade: "valor"}})
		return
	}

	payment, err := s.store.GetPayment(r.Context(), e2eID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		notFoundPix(w, "no payment with endToEndId "+e2eID)
		return
	case err != nil:
		s.log.Error("load payment", "e2eid", e2eID, "err", err)
		internalError(w)
		return
	}
	if amountCents != payment.AmountCents {
		badRefund(w, "partial refunds arrive in a later phase", []violacao{{
			Razao: fmt.Sprintf("valor must be the full settled amount, %s",
				core.FormatAmount(payment.AmountCents)),
			Propriedade: "valor",
		}})
		return
	}

	now := s.now()
	refund, created, err := s.store.CreateRefund(r.Context(), e2eID, id, amountCents, now,
		func(seq int64) (string, error) { return core.NewRtrID(s.rng, now, seq) })
	switch {
	case errors.Is(err, store.ErrRefundExceedsPayment):
		badRefund(w, "the refund could not be requested", []violacao{{
			Razao: fmt.Sprintf("refunds of %s already exhaust the %s that settled",
				core.FormatAmount(payment.RefundedCents), core.FormatAmount(payment.AmountCents)),
			Propriedade: "valor",
		}})
		return
	case err != nil:
		s.log.Error("create refund", "e2eid", e2eID, "id", id, "err", err)
		internalError(w)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated

		// Same decoupling as the pay path: the refund is committed, so the
		// reload and the callback survive the request that triggered them.
		ctx, cancel := notifyContext(r)
		defer cancel()
		payment, refunds, err := s.store.PaymentWithRefunds(ctx, e2eID)
		if err != nil {
			s.log.Error("reload payment", "e2eid", e2eID, "err", err)
		} else {
			s.log.Info("pix refunded", "e2eid", e2eID, "id", refund.ID, "rtrid", refund.RtrID)
			s.notify(ctx, payment, refunds)
		}
	}
	writeJSON(w, status, devolucaoDTO(refund))
}

// loadPayment fetches the payment named in the path along with its refunds,
// writing the error response itself when it fails.
func (s *Server) loadPayment(w http.ResponseWriter, r *http.Request) (core.Payment, []core.Refund, bool) {
	e2eID := chi.URLParam(r, "e2eid")
	if err := core.ValidateE2EID(e2eID); err != nil {
		notFoundPix(w, err.Error())
		return core.Payment{}, nil, false
	}

	payment, refunds, err := s.store.PaymentWithRefunds(r.Context(), e2eID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		notFoundPix(w, "no payment with endToEndId "+e2eID)
		return core.Payment{}, nil, false
	case err != nil:
		s.log.Error("load payment", "e2eid", e2eID, "err", err)
		internalError(w)
		return core.Payment{}, nil, false
	}
	return payment, refunds, true
}

// notifyTimeout bounds the store reads that feed a callback: detached from
// the request they would otherwise wait without any deadline at all.
const notifyTimeout = 5 * time.Second

// notifyContext detaches post-commit work from the request's cancellation
// while keeping its values, and bounds it with notifyTimeout so a detached
// read cannot hold the store's single connection forever.
func notifyContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.Context()), notifyTimeout)
}

// notify posts the payment to the endpoint registered for its key, if there is
// one. Delivery is asynchronous: the caller's response never waits on it, and
// a payee with no webhook is not an error.
func (s *Server) notify(ctx context.Context, p core.Payment, refunds []core.Refund) {
	hook, err := s.store.GetWebhook(ctx, p.Chave)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return
	case err != nil:
		s.log.Error("load webhook", "chave", p.Chave, "err", err)
		return
	}
	s.webhook.Deliver(store.PaymentAggregate(p.E2EID), hook.URL,
		webhookPayload{Pix: []pixResponse{pixDTO(p, refunds)}})
}

func pixDTO(p core.Payment, refunds []core.Refund) pixResponse {
	resp := pixResponse{
		EndToEndID:  p.E2EID,
		TxID:        p.TxID,
		Valor:       core.FormatAmount(p.AmountCents),
		Chave:       p.Chave,
		Horario:     p.CreatedAt.UTC().Format(bacenTime),
		InfoPagador: p.InfoPagador,
	}
	for _, r := range refunds {
		resp.Devolucoes = append(resp.Devolucoes, devolucaoDTO(r))
	}
	return resp
}

func devolucaoDTO(r core.Refund) devolucaoResponse {
	resp := devolucaoResponse{
		ID:      r.ID,
		RtrID:   r.RtrID,
		Valor:   core.FormatAmount(r.AmountCents),
		Horario: horarioDTO{Solicitacao: r.RequestedAt.UTC().Format(bacenTime)},
		Status:  string(r.Status),
		Motivo:  r.Motivo,
	}
	if !r.SettledAt.IsZero() {
		resp.Horario.Liquidacao = r.SettledAt.UTC().Format(bacenTime)
	}
	return resp
}
