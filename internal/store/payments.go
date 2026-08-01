package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/arinelliquebec/pix-sandbox/internal/core"
)

// Event types written by the payment lifecycle.
const (
	EventChargeSettled   = "cob.settled"
	EventPixReceived     = "pix.received"
	EventRefundRequested = "pix.devolucao.requested"
	EventRefundSettled   = "pix.devolucao.settled"
)

// PaymentAggregate is the event-log aggregate id for a payment.
func PaymentAggregate(e2eID string) string { return "pix:" + e2eID }

// Reasons a settlement or a refund can be refused. They are values rather than
// strings so the API layer can map each to its own status code.
var (
	ErrChargeSettled        = errors.New("store: charge already settled")
	ErrChargeNotPayable     = errors.New("store: charge is not payable")
	ErrRefundExceedsPayment = errors.New("store: refund exceeds the settled amount")
)

// Minter mints an identifier from the sequence number assigned to it. The
// sequence is drawn inside the transaction that stores the row, so two
// identifiers can never share one.
type Minter func(seq int64) (string, error)

const paymentColumns = `e2e_id, seq, txid, chave, status, amount_cents, refunded_cents,
	info_pagador, created_at`

const refundColumns = `id, e2e_id, rtr_id, seq, amount_cents, status, motivo, created_at, settled_at`

// SettleCharge records the payment of an active charge: it stores the pix,
// moves the charge to CONCLUIDA and logs both transitions in one transaction,
// so no state is reached without its event (INV-3).
//
// A charge that already settled comes back with ErrChargeSettled *and* the
// payment that settled it, so the caller can name it in the refusal. One that
// expired or was removed comes back with ErrChargeNotPayable.
func (s *Store) SettleCharge(ctx context.Context, txid, infoPagador string, now time.Time, mint Minter) (core.Payment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.Payment{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	charge, err := scanCharge(tx.QueryRowContext(ctx,
		`SELECT `+chargeColumns+` FROM charges WHERE txid = ?`, txid))
	if err != nil {
		return core.Payment{}, err
	}

	if charge.Status == core.StatusConcluida {
		existing, err := scanPayment(tx.QueryRowContext(ctx,
			`SELECT `+paymentColumns+` FROM payments WHERE txid = ?`, txid))
		if err != nil {
			return core.Payment{}, err
		}
		return existing, ErrChargeSettled
	}
	if charge.Status != core.StatusAtiva || charge.IsExpired(now) {
		return core.Payment{}, ErrChargeNotPayable
	}

	var seq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM payments`).Scan(&seq); err != nil {
		return core.Payment{}, fmt.Errorf("assign payment sequence: %w", err)
	}
	e2eID, err := mint(seq)
	if err != nil {
		return core.Payment{}, fmt.Errorf("mint e2eid: %w", err)
	}

	p := core.Payment{
		E2EID:       e2eID,
		Seq:         seq,
		TxID:        txid,
		Chave:       charge.Chave,
		Status:      core.PaymentSettled,
		AmountCents: charge.AmountCents,
		InfoPagador: infoPagador,
		CreatedAt:   now.UTC(),
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO payments (`+paymentColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.E2EID, p.Seq, p.TxID, p.Chave, string(p.Status), p.AmountCents, p.RefundedCents,
		p.InfoPagador, formatTime(p.CreatedAt),
	); err != nil {
		return core.Payment{}, fmt.Errorf("insert payment: %w", err)
	}

	// revisao tracks modifications of the charge, as BACEN specifies.
	if _, err := tx.ExecContext(ctx,
		`UPDATE charges SET status = ?, revisao = revisao + 1 WHERE txid = ?`,
		string(core.StatusConcluida), txid,
	); err != nil {
		return core.Payment{}, fmt.Errorf("settle charge: %w", err)
	}

	if err := appendEventTx(ctx, tx, PaymentAggregate(e2eID), EventPixReceived, paymentEventPayload(p)); err != nil {
		return core.Payment{}, err
	}
	if err := appendEventTx(ctx, tx, ChargeAggregate(txid), EventChargeSettled, map[string]any{
		"txid":         txid,
		"status":       string(core.StatusConcluida),
		"e2e_id":       e2eID,
		"amount_cents": p.AmountCents,
	}); err != nil {
		return core.Payment{}, err
	}

	if err := tx.Commit(); err != nil {
		return core.Payment{}, fmt.Errorf("commit: %w", err)
	}
	return p, nil
}

// CreateRefund raises a devolução against a payment and settles it.
//
// It is idempotent on (e2eId, id): replaying returns the stored refund with
// created false. The cap of INV-4 is checked here and again by a CHECK
// constraint on `payments`, so an accounting mistake cannot reach disk.
//
// Both transitions of the refund machine are logged — requested, then settled —
// even though in this phase they happen in the same transaction. The virtual
// clock and the chaos API will pull them apart; the log already has the shape.
func (s *Store) CreateRefund(ctx context.Context, e2eID, id string, amountCents int64, now time.Time, mint Minter) (core.Refund, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.Refund{}, false, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	payment, err := scanPayment(tx.QueryRowContext(ctx,
		`SELECT `+paymentColumns+` FROM payments WHERE e2e_id = ?`, e2eID))
	if err != nil {
		return core.Refund{}, false, err
	}

	existing, err := scanRefund(tx.QueryRowContext(ctx,
		`SELECT `+refundColumns+` FROM refunds WHERE e2e_id = ? AND id = ?`, e2eID, id))
	switch {
	case err == nil:
		return existing, false, nil
	case !errors.Is(err, ErrNotFound):
		return core.Refund{}, false, err
	}

	if amountCents > payment.RefundableCents() {
		return core.Refund{}, false, ErrRefundExceedsPayment
	}

	var seq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM refunds`).Scan(&seq); err != nil {
		return core.Refund{}, false, fmt.Errorf("assign refund sequence: %w", err)
	}
	rtrID, err := mint(seq)
	if err != nil {
		return core.Refund{}, false, fmt.Errorf("mint rtrid: %w", err)
	}

	refund := core.Refund{
		ID:          id,
		E2EID:       e2eID,
		RtrID:       rtrID,
		Seq:         seq,
		AmountCents: amountCents,
		Status:      core.RefundEmProcessamento,
		RequestedAt: now.UTC(),
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO refunds (`+refundColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		refund.ID, refund.E2EID, refund.RtrID, refund.Seq, refund.AmountCents,
		string(refund.Status), refund.Motivo, formatTime(refund.RequestedAt), "",
	); err != nil {
		return core.Refund{}, false, fmt.Errorf("insert refund: %w", err)
	}
	if err := appendEventTx(ctx, tx, PaymentAggregate(e2eID), EventRefundRequested, refundEventPayload(refund)); err != nil {
		return core.Refund{}, false, err
	}

	refund.Status = core.RefundDevolvido
	refund.SettledAt = now.UTC()
	if _, err := tx.ExecContext(ctx,
		`UPDATE refunds SET status = ?, settled_at = ? WHERE e2e_id = ? AND id = ?`,
		string(refund.Status), formatTime(refund.SettledAt), e2eID, id,
	); err != nil {
		return core.Refund{}, false, fmt.Errorf("settle refund: %w", err)
	}

	status := core.PaymentSettled
	if payment.RefundedCents+amountCents == payment.AmountCents {
		status = core.PaymentRefunded
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE payments SET refunded_cents = refunded_cents + ?, status = ? WHERE e2e_id = ?`,
		amountCents, string(status), e2eID,
	); err != nil {
		return core.Refund{}, false, fmt.Errorf("apply refund to payment: %w", err)
	}

	if err := appendEventTx(ctx, tx, PaymentAggregate(e2eID), EventRefundSettled, refundEventPayload(refund)); err != nil {
		return core.Refund{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return core.Refund{}, false, fmt.Errorf("commit: %w", err)
	}
	return refund, true, nil
}

// GetPayment returns the stored payment, or ErrNotFound.
func (s *Store) GetPayment(ctx context.Context, e2eID string) (core.Payment, error) {
	return scanPayment(s.db.QueryRowContext(ctx,
		`SELECT `+paymentColumns+` FROM payments WHERE e2e_id = ?`, e2eID))
}

// PaymentByTxID returns the payment that settled a charge, or ErrNotFound.
func (s *Store) PaymentByTxID(ctx context.Context, txid string) (core.Payment, error) {
	return scanPayment(s.db.QueryRowContext(ctx,
		`SELECT `+paymentColumns+` FROM payments WHERE txid = ?`, txid))
}

// PaymentWithRefunds returns a payment and its refunds, oldest first — the
// `Pix` resource as the API reports it.
func (s *Store) PaymentWithRefunds(ctx context.Context, e2eID string) (core.Payment, []core.Refund, error) {
	payment, err := s.GetPayment(ctx, e2eID)
	if err != nil {
		return core.Payment{}, nil, err
	}
	refunds, err := s.RefundsByPayment(ctx, e2eID)
	if err != nil {
		return core.Payment{}, nil, err
	}
	return payment, refunds, nil
}

// RefundsByPayment returns the refunds raised against a payment, oldest first.
func (s *Store) RefundsByPayment(ctx context.Context, e2eID string) ([]core.Refund, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+refundColumns+` FROM refunds WHERE e2e_id = ? ORDER BY seq`, e2eID)
	if err != nil {
		return nil, fmt.Errorf("query refunds: %w", err)
	}
	defer rows.Close()

	var out []core.Refund
	for rows.Next() {
		refund, err := scanRefund(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, refund)
	}
	return out, rows.Err()
}

func paymentEventPayload(p core.Payment) map[string]any {
	payload := map[string]any{
		"e2e_id":       p.E2EID,
		"txid":         p.TxID,
		"chave":        p.Chave,
		"status":       string(p.Status),
		"amount_cents": p.AmountCents,
		"horario":      formatTime(p.CreatedAt),
	}
	if p.InfoPagador != "" {
		payload["info_pagador"] = p.InfoPagador
	}
	return payload
}

func refundEventPayload(r core.Refund) map[string]any {
	payload := map[string]any{
		"id":           r.ID,
		"e2e_id":       r.E2EID,
		"rtr_id":       r.RtrID,
		"status":       string(r.Status),
		"amount_cents": r.AmountCents,
		"solicitacao":  formatTime(r.RequestedAt),
	}
	if !r.SettledAt.IsZero() {
		payload["liquidacao"] = formatTime(r.SettledAt)
	}
	if r.Motivo != "" {
		payload["motivo"] = r.Motivo
	}
	return payload
}

func scanPayment(row rowScanner) (core.Payment, error) {
	var (
		p         core.Payment
		status    string
		createdAt string
	)
	err := row.Scan(&p.E2EID, &p.Seq, &p.TxID, &p.Chave, &status, &p.AmountCents,
		&p.RefundedCents, &p.InfoPagador, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return core.Payment{}, ErrNotFound
	}
	if err != nil {
		return core.Payment{}, fmt.Errorf("scan payment: %w", err)
	}

	p.Status = core.PaymentStatus(status)
	if p.CreatedAt, err = parseTime(createdAt); err != nil {
		return core.Payment{}, err
	}
	return p, nil
}

func scanRefund(row rowScanner) (core.Refund, error) {
	var (
		r         core.Refund
		status    string
		createdAt string
		settledAt string
	)
	err := row.Scan(&r.ID, &r.E2EID, &r.RtrID, &r.Seq, &r.AmountCents,
		&status, &r.Motivo, &createdAt, &settledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return core.Refund{}, ErrNotFound
	}
	if err != nil {
		return core.Refund{}, fmt.Errorf("scan refund: %w", err)
	}

	r.Status = core.RefundStatus(status)
	if r.RequestedAt, err = parseTime(createdAt); err != nil {
		return core.Refund{}, err
	}
	if settledAt != "" {
		if r.SettledAt, err = parseTime(settledAt); err != nil {
			return core.Refund{}, err
		}
	}
	return r, nil
}
