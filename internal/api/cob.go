package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/arinellidu/pix-sandbox-pay/internal/core"
	"github.com/arinellidu/pix-sandbox-pay/internal/emv"
	"github.com/arinellidu/pix-sandbox-pay/internal/store"
)

// bacenTime is the timestamp format the API Pix uses: UTC, milliseconds.
const bacenTime = "2006-01-02T15:04:05.000Z"

type cobRequest struct {
	Calendario *struct {
		Expiracao *int64 `json:"expiracao"`
	} `json:"calendario"`
	Devedor *struct {
		CPF  string `json:"cpf"`
		CNPJ string `json:"cnpj"`
		Nome string `json:"nome"`
	} `json:"devedor"`
	Valor *struct {
		Original string `json:"original"`
	} `json:"valor"`
	Chave              string `json:"chave"`
	SolicitacaoPagador string `json:"solicitacaoPagador"`
	// TxID is not part of BACEN's POST /cob body — there the txid is always
	// server-assigned. The sandbox accepts it so a caller can pin one without
	// switching to PUT, which makes curl-driven demos and fixtures simpler.
	TxID string `json:"txid"`
}

type calendarioDTO struct {
	Criacao   string `json:"criacao"`
	Expiracao int64  `json:"expiracao"`
}

type valorDTO struct {
	Original string `json:"original"`
}

type locDTO struct {
	ID       int64  `json:"id"`
	Location string `json:"location"`
	TipoCob  string `json:"tipoCob"`
}

type devedorDTO struct {
	CPF  string `json:"cpf,omitempty"`
	CNPJ string `json:"cnpj,omitempty"`
	Nome string `json:"nome,omitempty"`
}

type cobResponse struct {
	Calendario         calendarioDTO `json:"calendario"`
	TxID               string        `json:"txid"`
	Revisao            int64         `json:"revisao"`
	Loc                locDTO        `json:"loc"`
	Location           string        `json:"location"`
	Status             string        `json:"status"`
	Devedor            *devedorDTO   `json:"devedor,omitempty"`
	Valor              valorDTO      `json:"valor"`
	Chave              string        `json:"chave"`
	SolicitacaoPagador string        `json:"solicitacaoPagador,omitempty"`
	PixCopiaECola      string        `json:"pixCopiaECola"`
}

type qrCodeResponse struct {
	QRCode string `json:"qrcode"`
	// ImagemQrcode is always null here: rendering the image is a later phase.
	ImagemQrcode *string `json:"imagemQrcode"`
}

// handleCreateCob serves POST /cob and PUT /cob/{txid}.
//
// It is idempotent on txid (INV-2): replaying either verb with a txid that
// already exists returns the stored charge, with 200 rather than 201 so the
// caller can tell a replay from a creation.
func (s *Server) handleCreateCob(w http.ResponseWriter, r *http.Request) {
	var req cobRequest
	if err := decodeJSON(r, &req); err != nil {
		malformedRequest(w, err.Error())
		return
	}

	pathTxID := chi.URLParam(r, "txid")
	txid := pathTxID
	if txid == "" {
		txid = req.TxID
	}
	if pathTxID != "" && req.TxID != "" && req.TxID != pathTxID {
		badRequest(w, "txid in the body does not match the one in the path", []violacao{
			{Razao: "o txid do corpo diverge do txid da URL", Propriedade: "txid"},
		})
		return
	}

	minted := txid == ""
	charge, violacoes := s.buildCharge(txid, req)
	if len(violacoes) > 0 {
		badRequest(w, "the charge could not be created", violacoes)
		return
	}

	stored, created, err := s.store.CreateCharge(r.Context(), charge)
	if err != nil {
		s.log.Error("create charge", "txid", charge.TxID, "err", err)
		internalError(w)
		return
	}

	// A replay is idempotency only when the caller chose the txid. A minted
	// txid that already exists is a collision with a previous run of the same
	// seed — the rng restarts on boot, the database does not — so handing back
	// the stored charge would silently serve another run's data. Mint again:
	// each draw advances the seeded stream, and the collided prefix is at most
	// as long as the table.
	for minted && !created {
		charge, violacoes = s.buildCharge("", req)
		if len(violacoes) > 0 {
			badRequest(w, "the charge could not be created", violacoes)
			return
		}
		stored, created, err = s.store.CreateCharge(r.Context(), charge)
		if err != nil {
			s.log.Error("create charge", "txid", charge.TxID, "err", err)
			internalError(w)
			return
		}
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, s.cobResponse(stored, s.now()))
}

// buildCharge validates the request and assembles the charge, including its
// BR Code. It returns every violation it finds rather than the first.
func (s *Server) buildCharge(txid string, req cobRequest) (core.Charge, []violacao) {
	var violacoes []violacao

	if txid == "" {
		txid = core.NewTxID(s.rng)
	} else if err := core.ValidateTxID(txid); err != nil {
		violacoes = append(violacoes, violacao{Razao: err.Error(), Propriedade: "txid"})
	}

	var amountCents int64
	switch {
	case req.Valor == nil || req.Valor.Original == "":
		violacoes = append(violacoes, violacao{Razao: "valor.original é obrigatório", Propriedade: "valor.original"})
	default:
		cents, err := core.ParseAmount(req.Valor.Original)
		if err != nil {
			violacoes = append(violacoes, violacao{Razao: err.Error(), Propriedade: "valor.original"})
		}
		// BACEN rejects a zero-value charge, and so does the refund schema
		// downstream — a 0-cent payment would be impossible to devolve.
		if err == nil && cents == 0 {
			violacoes = append(violacoes, violacao{
				Razao:       "valor.original must be greater than zero",
				Propriedade: "valor.original",
			})
		}
		amountCents = cents
	}

	if err := core.ValidateChave(req.Chave); err != nil {
		violacoes = append(violacoes, violacao{Razao: err.Error(), Propriedade: "chave"})
	}

	expiracao := core.DefaultExpiracao
	if req.Calendario != nil && req.Calendario.Expiracao != nil {
		expiracao = *req.Calendario.Expiracao
		if expiracao <= 0 || expiracao > core.MaxExpiracao {
			violacoes = append(violacoes, violacao{
				Razao:       fmt.Sprintf("calendario.expiracao must be between 1 and %d seconds", core.MaxExpiracao),
				Propriedade: "calendario.expiracao",
			})
		}
	}

	if len(req.SolicitacaoPagador) > core.MaxSolicitacaoPagadorLen {
		violacoes = append(violacoes, violacao{
			Razao:       fmt.Sprintf("solicitacaoPagador must be at most %d characters", core.MaxSolicitacaoPagadorLen),
			Propriedade: "solicitacaoPagador",
		})
	}

	devedor, devedorViolacoes := parseDevedor(req)
	violacoes = append(violacoes, devedorViolacoes...)

	if len(violacoes) > 0 {
		return core.Charge{}, violacoes
	}

	now := s.now()
	charge := core.Charge{
		TxID:               txid,
		Status:             core.StatusAtiva,
		AmountCents:        amountCents,
		Chave:              req.Chave,
		SolicitacaoPagador: req.SolicitacaoPagador,
		Devedor:            devedor,
		Expiracao:          expiracao,
		CreatedAt:          now,
		ExpiresAt:          now.Add(time.Duration(expiracao) * time.Second),
	}

	payload, err := emv.BRCode{
		Key:          charge.Chave,
		TxID:         charge.TxID,
		Amount:       core.FormatAmount(charge.AmountCents),
		MerchantName: s.cfg.MerchantName,
		MerchantCity: s.cfg.MerchantCity,
	}.Payload()
	if err != nil {
		return core.Charge{}, []violacao{{Razao: err.Error(), Propriedade: "chave"}}
	}
	charge.EMV = payload

	return charge, nil
}

func parseDevedor(req cobRequest) (*core.Devedor, []violacao) {
	if req.Devedor == nil {
		return nil, nil
	}

	var violacoes []violacao
	d := req.Devedor

	switch {
	case d.CPF != "" && d.CNPJ != "":
		violacoes = append(violacoes, violacao{
			Razao:       "devedor must carry either cpf or cnpj, not both",
			Propriedade: "devedor",
		})
	case d.CPF != "":
		if err := core.ValidateDocument(d.CPF, 11, "cpf"); err != nil {
			violacoes = append(violacoes, violacao{Razao: err.Error(), Propriedade: "devedor.cpf"})
		}
	case d.CNPJ != "":
		if err := core.ValidateDocument(d.CNPJ, 14, "cnpj"); err != nil {
			violacoes = append(violacoes, violacao{Razao: err.Error(), Propriedade: "devedor.cnpj"})
		}
	}
	if d.Nome == "" {
		violacoes = append(violacoes, violacao{Razao: "devedor.nome é obrigatório", Propriedade: "devedor.nome"})
	}

	if len(violacoes) > 0 {
		return nil, violacoes
	}
	return &core.Devedor{Nome: d.Nome, CPF: d.CPF, CNPJ: d.CNPJ}, nil
}

// handleGetCob serves GET /cob/{txid}.
func (s *Server) handleGetCob(w http.ResponseWriter, r *http.Request) {
	charge, ok := s.loadCharge(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.cobResponse(charge, s.now()))
}

// handleGetCobQRCode serves GET /cob/{txid}/qrcode.
func (s *Server) handleGetCobQRCode(w http.ResponseWriter, r *http.Request) {
	charge, ok := s.loadCharge(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, qrCodeResponse{QRCode: charge.EMV, ImagemQrcode: nil})
}

// loadCharge fetches the charge named in the path, settling any pending
// expiry first so the log records the transition rather than the reader
// inferring it (INV-3). It writes the error response itself when it fails.
func (s *Server) loadCharge(w http.ResponseWriter, r *http.Request) (core.Charge, bool) {
	txid := chi.URLParam(r, "txid")
	if err := core.ValidateTxID(txid); err != nil {
		notFound(w, err.Error())
		return core.Charge{}, false
	}

	charge, err := s.store.ExpireCharge(r.Context(), txid, s.now())
	switch {
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "no charge with txid "+txid)
		return core.Charge{}, false
	case err != nil:
		s.log.Error("load charge", "txid", txid, "err", err)
		internalError(w)
		return core.Charge{}, false
	}
	return charge, true
}

func (s *Server) cobResponse(c core.Charge, now time.Time) cobResponse {
	resp := cobResponse{
		Calendario: calendarioDTO{
			Criacao:   c.CreatedAt.UTC().Format(bacenTime),
			Expiracao: c.Expiracao,
		},
		TxID:    c.TxID,
		Revisao: c.Revisao,
		Loc: locDTO{
			ID:       c.LocID,
			Location: s.location(c.LocID),
			TipoCob:  "cob",
		},
		Location:           s.location(c.LocID),
		Status:             string(c.EffectiveStatus(now)),
		Valor:              valorDTO{Original: core.FormatAmount(c.AmountCents)},
		Chave:              c.Chave,
		SolicitacaoPagador: c.SolicitacaoPagador,
		PixCopiaECola:      c.EMV,
	}
	if c.Devedor != nil {
		resp.Devedor = &devedorDTO{Nome: c.Devedor.Nome, CPF: c.Devedor.CPF, CNPJ: c.Devedor.CNPJ}
	}
	return resp
}

// location mirrors BACEN's payload location: a scheme-less URL the payer's app
// would fetch. Self-contained BR Codes do not need it, so it is informational
// until the location endpoint exists.
func (s *Server) location(locID int64) string {
	return fmt.Sprintf("%s/qr/v2/%d", s.cfg.BaseURL, locID)
}

// decodeJSON reads a JSON body, tolerating an empty one so a caller can PUT a
// charge whose fields are all defaults and still get a readable validation
// error instead of a parse error.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("malformed JSON body: %w", err)
	}
	return nil
}
