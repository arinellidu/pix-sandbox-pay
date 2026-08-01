package api

import (
	"net/http"
)

// Errors follow RFC 7807, the shape BACEN's API Pix returns. The `type` URIs
// mirror the ones a real PSP emits so a client's error handling exercises the
// same branches; the sandbox does not claim to reproduce every slug in the
// specification, only the ones its endpoints can reach.
const problemBase = "https://pix.bcb.gov.br/api/v2/error/"

const (
	problemCobOperacaoInvalida       = "CobOperacaoInvalida"
	problemCobNaoEncontrada          = "CobNaoEncontrada"
	problemPixNaoEncontrado          = "PixNaoEncontrado"
	problemDevolucaoOperacaoInvalida = "DevolucaoOperacaoInvalida"
	problemWebhookOperacaoInvalida   = "WebhookOperacaoInvalida"
	problemWebhookNaoEncontrado      = "WebhookNaoEncontrado"
	problemRequisicaoInvalida        = "RequisicaoInvalida"
	problemErroInterno               = "ErroInternoDoServidor"
)

// violacao is one field-level complaint inside a problem document.
type violacao struct {
	Razao       string `json:"razao"`
	Propriedade string `json:"propriedade,omitempty"`
}

// problem is an RFC 7807 document.
type problem struct {
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Status    int        `json:"status"`
	Detail    string     `json:"detail,omitempty"`
	Violacoes []violacao `json:"violacoes,omitempty"`
}

func writeProblem(w http.ResponseWriter, status int, slug, title, detail string, violacoes []violacao) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	encodeJSON(w, problem{
		Type:      problemBase + slug,
		Title:     title,
		Status:    status,
		Detail:    detail,
		Violacoes: violacoes,
	})
}

func badRequest(w http.ResponseWriter, detail string, violacoes []violacao) {
	writeProblem(w, http.StatusBadRequest, problemCobOperacaoInvalida,
		"Cobrança inválida.", detail, violacoes)
}

func malformedRequest(w http.ResponseWriter, detail string) {
	writeProblem(w, http.StatusBadRequest, problemRequisicaoInvalida,
		"Requisição inválida.", detail, nil)
}

func notFound(w http.ResponseWriter, detail string) {
	writeProblem(w, http.StatusNotFound, problemCobNaoEncontrada,
		"Cobrança não encontrada.", detail, nil)
}

// conflict refuses an operation the charge's state machine forbids — paying a
// charge that already settled. BACEN has no slug for it because no BACEN
// endpoint can reach it; the sandbox reuses the charge one and says why.
func conflict(w http.ResponseWriter, detail string) {
	writeProblem(w, http.StatusConflict, problemCobOperacaoInvalida,
		"Cobrança inválida.", detail, nil)
}

func notFoundPix(w http.ResponseWriter, detail string) {
	writeProblem(w, http.StatusNotFound, problemPixNaoEncontrado,
		"Pix não encontrado.", detail, nil)
}

func badRefund(w http.ResponseWriter, detail string, violacoes []violacao) {
	writeProblem(w, http.StatusBadRequest, problemDevolucaoOperacaoInvalida,
		"Devolução inválida.", detail, violacoes)
}

func badWebhook(w http.ResponseWriter, detail string, violacoes []violacao) {
	writeProblem(w, http.StatusBadRequest, problemWebhookOperacaoInvalida,
		"Webhook inválido.", detail, violacoes)
}

func notFoundWebhook(w http.ResponseWriter, detail string) {
	writeProblem(w, http.StatusNotFound, problemWebhookNaoEncontrado,
		"Webhook não encontrado.", detail, nil)
}

func internalError(w http.ResponseWriter) {
	writeProblem(w, http.StatusInternalServerError, problemErroInterno,
		"Erro interno do servidor.", "", nil)
}
