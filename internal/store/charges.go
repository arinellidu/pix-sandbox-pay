package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/arinelliquebec/pix-sandbox/internal/core"
)

// Event types written by the charge lifecycle.
const (
	EventChargeCreated = "cob.created"
	EventChargeExpired = "cob.expired"
)

// ChargeAggregate is the event-log aggregate id for a charge.
func ChargeAggregate(txid string) string { return "cob:" + txid }

const chargeColumns = `txid, status, amount_cents, chave, emv, created_at, expires_at,
	solicitacao_pagador, devedor_nome, devedor_cpf, devedor_cnpj, expiracao, loc_id, revisao`

// CreateCharge stores a new charge and logs cob.created.
//
// It is idempotent on txid (INV-2): if the txid already exists, the stored
// charge is returned untouched and created is false. Lookup, insert and event
// share one transaction, so a replay can never produce a second row or a
// second event.
func (s *Store) CreateCharge(ctx context.Context, c core.Charge) (charge core.Charge, created bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.Charge{}, false, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	existing, err := scanCharge(tx.QueryRowContext(ctx,
		`SELECT `+chargeColumns+` FROM charges WHERE txid = ?`, c.TxID))
	switch {
	case err == nil:
		return existing, false, nil
	case !errors.Is(err, ErrNotFound):
		return core.Charge{}, false, err
	}

	// loc.id mirrors BACEN's payload location handle: a small monotonic
	// integer, assigned inside the transaction so it cannot collide.
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(loc_id), 0) + 1 FROM charges`).Scan(&c.LocID); err != nil {
		return core.Charge{}, false, fmt.Errorf("assign loc id: %w", err)
	}

	var devedorNome, devedorCPF, devedorCNPJ string
	if c.Devedor != nil {
		devedorNome, devedorCPF, devedorCNPJ = c.Devedor.Nome, c.Devedor.CPF, c.Devedor.CNPJ
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO charges (`+chargeColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.TxID, string(c.Status), c.AmountCents, c.Chave, c.EMV,
		formatTime(c.CreatedAt), formatTime(c.ExpiresAt),
		c.SolicitacaoPagador, devedorNome, devedorCPF, devedorCNPJ,
		c.Expiracao, c.LocID, c.Revisao,
	); err != nil {
		return core.Charge{}, false, fmt.Errorf("insert charge: %w", err)
	}

	if err := appendEventTx(ctx, tx, ChargeAggregate(c.TxID), EventChargeCreated, chargeEventPayload(c)); err != nil {
		return core.Charge{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return core.Charge{}, false, fmt.Errorf("commit: %w", err)
	}
	return c, true, nil
}

// GetCharge returns the stored charge, or ErrNotFound.
func (s *Store) GetCharge(ctx context.Context, txid string) (core.Charge, error) {
	return scanCharge(s.db.QueryRowContext(ctx,
		`SELECT `+chargeColumns+` FROM charges WHERE txid = ?`, txid))
}

// ExpireCharge moves an active charge past its window to EXPIRADA and logs
// cob.expired. A charge that is not active, or not yet past its window, is
// returned unchanged — so calling this on every read is safe.
func (s *Store) ExpireCharge(ctx context.Context, txid string, now time.Time) (core.Charge, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.Charge{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	charge, err := scanCharge(tx.QueryRowContext(ctx,
		`SELECT `+chargeColumns+` FROM charges WHERE txid = ?`, txid))
	if err != nil {
		return core.Charge{}, err
	}
	if !charge.IsExpired(now) {
		return charge, nil
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE charges SET status = ? WHERE txid = ?`, string(core.StatusExpirada), txid,
	); err != nil {
		return core.Charge{}, fmt.Errorf("expire charge: %w", err)
	}
	charge.Status = core.StatusExpirada

	if err := appendEventTx(ctx, tx, ChargeAggregate(txid), EventChargeExpired, map[string]any{
		"txid":        txid,
		"expires_at":  formatTime(charge.ExpiresAt),
		"observed_at": formatTime(now),
	}); err != nil {
		return core.Charge{}, err
	}

	if err := tx.Commit(); err != nil {
		return core.Charge{}, fmt.Errorf("commit: %w", err)
	}
	return charge, nil
}

// chargeEventPayload is what lands in the log for a charge transition. Money
// stays in cents here too: the log speaks the domain's language, not the API's.
func chargeEventPayload(c core.Charge) map[string]any {
	payload := map[string]any{
		"txid":         c.TxID,
		"status":       string(c.Status),
		"amount_cents": c.AmountCents,
		"chave":        c.Chave,
		"expiracao":    c.Expiracao,
		"loc_id":       c.LocID,
		"created_at":   formatTime(c.CreatedAt),
		"expires_at":   formatTime(c.ExpiresAt),
	}
	if c.SolicitacaoPagador != "" {
		payload["solicitacao_pagador"] = c.SolicitacaoPagador
	}
	if c.Devedor != nil {
		devedor := map[string]any{"nome": c.Devedor.Nome}
		if c.Devedor.CPF != "" {
			devedor["cpf"] = c.Devedor.CPF
		}
		if c.Devedor.CNPJ != "" {
			devedor["cnpj"] = c.Devedor.CNPJ
		}
		payload["devedor"] = devedor
	}
	return payload
}

// rowScanner covers both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCharge(row rowScanner) (core.Charge, error) {
	var (
		c           core.Charge
		status      string
		createdAt   string
		expiresAt   string
		devedorNome string
		devedorCPF  string
		devedorCNPJ string
	)
	err := row.Scan(
		&c.TxID, &status, &c.AmountCents, &c.Chave, &c.EMV, &createdAt, &expiresAt,
		&c.SolicitacaoPagador, &devedorNome, &devedorCPF, &devedorCNPJ,
		&c.Expiracao, &c.LocID, &c.Revisao,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return core.Charge{}, ErrNotFound
	}
	if err != nil {
		return core.Charge{}, fmt.Errorf("scan charge: %w", err)
	}

	c.Status = core.Status(status)
	if c.CreatedAt, err = parseTime(createdAt); err != nil {
		return core.Charge{}, err
	}
	if c.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return core.Charge{}, err
	}
	if devedorNome != "" || devedorCPF != "" || devedorCNPJ != "" {
		c.Devedor = &core.Devedor{Nome: devedorNome, CPF: devedorCPF, CNPJ: devedorCNPJ}
	}
	return c, nil
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}
