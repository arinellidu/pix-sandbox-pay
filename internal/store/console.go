package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/arinellidu/pix-sandbox-pay/internal/core"
)

// Reads that exist for the console. They are queries over the same projections
// the API serves, never a second source of truth.

// Stats counts what the run has produced so far.
type Stats struct {
	Charges  int64
	Payments int64
	Refunds  int64
	Webhooks int64
	Events   int64
}

// Stats counts the rows behind the console's header.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var st Stats
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM charges),
		(SELECT COUNT(*) FROM payments),
		(SELECT COUNT(*) FROM refunds),
		(SELECT COUNT(*) FROM webhooks),
		(SELECT COUNT(*) FROM events)`).Scan(
		&st.Charges, &st.Payments, &st.Refunds, &st.Webhooks, &st.Events)
	if err != nil {
		return Stats{}, fmt.Errorf("read stats: %w", err)
	}
	return st, nil
}

// ListCharges returns the most recent charges, newest first.
func (s *Store) ListCharges(ctx context.Context, limit int) ([]core.Charge, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+chargeColumns+` FROM charges ORDER BY created_at DESC, txid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query charges: %w", err)
	}
	defer rows.Close()

	var out []core.Charge
	for rows.Next() {
		charge, err := scanCharge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, charge)
	}
	return out, rows.Err()
}

// ChargeTimeline returns every event recorded for a charge and for the payment
// that settled it, oldest first — the whole story of one txid in one list.
//
// The two aggregates stay separate in the log because they are separate
// things; joining them is a reader's concern, which is why it happens here and
// not at write time.
func (s *Store) ChargeTimeline(ctx context.Context, txid string) ([]Event, error) {
	aggregates := []any{ChargeAggregate(txid)}

	payment, err := s.PaymentByTxID(ctx, txid)
	switch {
	case err == nil:
		aggregates = append(aggregates, PaymentAggregate(payment.E2EID))
	case !errors.Is(err, ErrNotFound):
		return nil, err
	}

	query := `SELECT id, aggregate, type, payload, created_at FROM events WHERE aggregate IN (?`
	for range aggregates[1:] {
		query += `, ?`
	}
	query += `) ORDER BY id`

	rows, err := s.db.QueryContext(ctx, query, aggregates...)
	if err != nil {
		return nil, fmt.Errorf("query timeline: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}
