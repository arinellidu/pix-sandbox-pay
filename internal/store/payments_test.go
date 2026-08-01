package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/arinellidu/pix-sandbox-pay/internal/core"
	"github.com/arinellidu/pix-sandbox-pay/internal/store"
)

var settledAt = time.Date(2026, 7, 31, 12, 30, 0, 0, time.UTC)

// mintSeq is a stand-in for the real minter: it only has to be injective on
// the sequence, which is exactly what the store guarantees it is given.
func mintSeq(prefix string) store.Minter {
	return func(seq int64) (string, error) {
		return fmt.Sprintf("%s%08d%012d%011d", prefix, 12345678, 202607311230, seq), nil
	}
}

// settledCharge stores an active charge and pays it.
func settledCharge(t *testing.T, st *store.Store) core.Payment {
	t.Helper()
	ctx := context.Background()

	// Well inside the sample charge's one-hour window.
	created := settledAt.Add(-30 * time.Minute)
	if _, _, err := st.CreateCharge(ctx, sampleCharge(sampleTxID, created)); err != nil {
		t.Fatalf("CreateCharge: %v", err)
	}
	payment, err := st.SettleCharge(ctx, sampleTxID, "Bought a coffee", settledAt, mintSeq("E"))
	if err != nil {
		t.Fatalf("SettleCharge: %v", err)
	}
	return payment
}

func TestSettleChargeStoresPaymentAndClosesCharge(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	payment := settledCharge(t, st)
	if payment.AmountCents != 1000 {
		t.Errorf("amount_cents = %d, want the charge's 1000", payment.AmountCents)
	}
	if payment.Chave != "dev@example.com" {
		t.Errorf("chave = %q, want the charge's key", payment.Chave)
	}
	if payment.Status != core.PaymentSettled {
		t.Errorf("status = %q, want SETTLED", payment.Status)
	}
	if payment.Seq != 1 {
		t.Errorf("seq = %d, want 1", payment.Seq)
	}
	if !payment.CreatedAt.Equal(settledAt) {
		t.Errorf("horario = %s, want %s", payment.CreatedAt, settledAt)
	}

	charge, err := st.GetCharge(ctx, sampleTxID)
	if err != nil {
		t.Fatalf("GetCharge: %v", err)
	}
	if charge.Status != core.StatusConcluida {
		t.Errorf("charge status = %q, want CONCLUIDA", charge.Status)
	}
	if charge.Revisao != 1 {
		t.Errorf("revisao = %d, want 1 after the charge was modified", charge.Revisao)
	}

	read, err := st.GetPayment(ctx, payment.E2EID)
	if err != nil {
		t.Fatalf("GetPayment: %v", err)
	}
	if read.InfoPagador != "Bought a coffee" {
		t.Errorf("info_pagador = %q", read.InfoPagador)
	}
	byTxID, err := st.PaymentByTxID(ctx, sampleTxID)
	if err != nil || byTxID.E2EID != payment.E2EID {
		t.Errorf("PaymentByTxID = %+v, %v", byTxID, err)
	}
}

// INV-3: the payment and the charge transition both reach the log, on their
// own aggregates, in the same transaction.
func TestSettleChargeLogsBothTransitions(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	payment := settledCharge(t, st)

	pixEvents, err := st.EventsByAggregate(ctx, store.PaymentAggregate(payment.E2EID))
	if err != nil {
		t.Fatalf("EventsByAggregate: %v", err)
	}
	if len(pixEvents) != 1 || pixEvents[0].Type != store.EventPixReceived {
		t.Fatalf("pix events = %+v, want one pix.received", pixEvents)
	}

	cobEvents, err := st.EventsByAggregate(ctx, store.ChargeAggregate(sampleTxID))
	if err != nil {
		t.Fatalf("EventsByAggregate: %v", err)
	}
	if len(cobEvents) != 2 || cobEvents[1].Type != store.EventChargeSettled {
		t.Fatalf("cob events = %+v, want created then settled", cobEvents)
	}
}

// A charge settles once: the second attempt is refused and names the payment
// that already exists, and nothing new lands in the log.
func TestSettleChargeRefusesASecondPayment(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	first := settledCharge(t, st)

	second, err := st.SettleCharge(ctx, sampleTxID, "", settledAt, mintSeq("E"))
	if !errors.Is(err, store.ErrChargeSettled) {
		t.Fatalf("err = %v, want ErrChargeSettled", err)
	}
	if second.E2EID != first.E2EID {
		t.Errorf("refusal named %q, want the original %q", second.E2EID, first.E2EID)
	}

	var rows int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&rows); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if rows != 1 {
		t.Errorf("payments rows = %d, want 1", rows)
	}
}

func TestSettleChargeRefusesExpiredAndMissing(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	if _, err := st.SettleCharge(ctx, sampleTxID, "", settledAt, mintSeq("E")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	created := settledAt.Add(-2 * time.Hour) // the sample charge lives an hour
	if _, _, err := st.CreateCharge(ctx, sampleCharge(sampleTxID, created)); err != nil {
		t.Fatalf("CreateCharge: %v", err)
	}
	if _, err := st.SettleCharge(ctx, sampleTxID, "", settledAt, mintSeq("E")); !errors.Is(err, store.ErrChargeNotPayable) {
		t.Fatalf("err = %v, want ErrChargeNotPayable", err)
	}
}

func TestCreateRefundSettlesAndLogsBothStates(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	payment := settledCharge(t, st)

	refund, created, err := st.CreateRefund(ctx, payment.E2EID, "dev1", 1000, settledAt, mintSeq("D"))
	if err != nil {
		t.Fatalf("CreateRefund: %v", err)
	}
	if !created {
		t.Fatal("created = false on the first refund")
	}
	if refund.Status != core.RefundDevolvido {
		t.Errorf("status = %q, want DEVOLVIDO", refund.Status)
	}
	if refund.SettledAt.IsZero() {
		t.Error("liquidacao is unset on a settled refund")
	}

	read, err := st.GetPayment(ctx, payment.E2EID)
	if err != nil {
		t.Fatalf("GetPayment: %v", err)
	}
	if read.RefundedCents != 1000 {
		t.Errorf("refunded_cents = %d, want 1000", read.RefundedCents)
	}
	if read.Status != core.PaymentRefunded {
		t.Errorf("payment status = %q, want REFUNDED once it is whole", read.Status)
	}

	// INV-3: the refund walked its machine in the log rather than appearing
	// already settled.
	events, err := st.EventsByAggregate(ctx, store.PaymentAggregate(payment.E2EID))
	if err != nil {
		t.Fatalf("EventsByAggregate: %v", err)
	}
	want := []string{store.EventPixReceived, store.EventRefundRequested, store.EventRefundSettled}
	if len(events) != len(want) {
		t.Fatalf("events = %+v, want %v", events, want)
	}
	for i, typ := range want {
		if events[i].Type != typ {
			t.Errorf("event %d = %q, want %q", i, events[i].Type, typ)
		}
	}
}

// INV-2: replaying a refund id returns the stored refund and creates nothing.
func TestCreateRefundIsIdempotent(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	payment := settledCharge(t, st)
	first, _, err := st.CreateRefund(ctx, payment.E2EID, "dev1", 1000, settledAt, mintSeq("D"))
	if err != nil {
		t.Fatalf("CreateRefund: %v", err)
	}

	replay, created, err := st.CreateRefund(ctx, payment.E2EID, "dev1", 1000, settledAt.Add(time.Hour), mintSeq("D"))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if created {
		t.Error("created = true on a replay")
	}
	if replay.RtrID != first.RtrID {
		t.Errorf("replay rtrId = %q, want the original %q", replay.RtrID, first.RtrID)
	}

	read, err := st.GetPayment(ctx, payment.E2EID)
	if err != nil {
		t.Fatalf("GetPayment: %v", err)
	}
	if read.RefundedCents != 1000 {
		t.Errorf("refunded_cents = %d after a replay, want 1000", read.RefundedCents)
	}
}

// INV-4: refunds never exceed what settled, whether asked for at once or
// accumulated across ids.
func TestCreateRefundIsBoundedByTheSettledAmount(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	payment := settledCharge(t, st)

	if _, _, err := st.CreateRefund(ctx, payment.E2EID, "toobig", 1001, settledAt, mintSeq("D")); !errors.Is(err, store.ErrRefundExceedsPayment) {
		t.Fatalf("err = %v, want ErrRefundExceedsPayment", err)
	}

	if _, _, err := st.CreateRefund(ctx, payment.E2EID, "half", 500, settledAt, mintSeq("D")); err != nil {
		t.Fatalf("first half: %v", err)
	}
	if _, _, err := st.CreateRefund(ctx, payment.E2EID, "rest", 501, settledAt, mintSeq("D")); !errors.Is(err, store.ErrRefundExceedsPayment) {
		t.Fatalf("err = %v, want ErrRefundExceedsPayment on the overflowing half", err)
	}
	if _, _, err := st.CreateRefund(ctx, payment.E2EID, "rest", 500, settledAt, mintSeq("D")); err != nil {
		t.Fatalf("exact remainder: %v", err)
	}

	read, err := st.GetPayment(ctx, payment.E2EID)
	if err != nil {
		t.Fatalf("GetPayment: %v", err)
	}
	if read.RefundedCents != read.AmountCents {
		t.Errorf("refunded_cents = %d, want the full %d", read.RefundedCents, read.AmountCents)
	}
	if read.RefundableCents() != 0 {
		t.Errorf("RefundableCents() = %d, want 0", read.RefundableCents())
	}
}

// The invariant is the database's too: a hand-written UPDATE that would break
// it is refused, not merely discouraged.
func TestRefundCapIsEnforcedBySchema(t *testing.T) {
	st := newStore(t)

	payment := settledCharge(t, st)

	_, err := st.DB().Exec(`UPDATE payments SET refunded_cents = amount_cents + 1 WHERE e2e_id = ?`, payment.E2EID)
	if err == nil {
		t.Fatal("the database accepted refunds above the settled amount")
	}
}

func TestRefundsByPaymentIsOrdered(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	payment := settledCharge(t, st)
	for _, id := range []string{"a", "b"} {
		if _, _, err := st.CreateRefund(ctx, payment.E2EID, id, 500, settledAt, mintSeq("D")); err != nil {
			t.Fatalf("CreateRefund(%q): %v", id, err)
		}
	}

	read, refunds, err := st.PaymentWithRefunds(ctx, payment.E2EID)
	if err != nil {
		t.Fatalf("PaymentWithRefunds: %v", err)
	}
	if read.RefundedCents != 1000 {
		t.Errorf("refunded_cents = %d, want 1000", read.RefundedCents)
	}
	if len(refunds) != 2 || refunds[0].ID != "a" || refunds[1].ID != "b" {
		t.Errorf("refunds = %+v, want a then b", refunds)
	}
}

func TestGetPaymentNotFound(t *testing.T) {
	st := newStore(t)

	if _, err := st.GetPayment(context.Background(), "E12345678202607311204x7k2q90000f"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestPutWebhookRegistersAndReplaces(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	hook, created, err := st.PutWebhook(ctx, "dev@example.com", "https://example.com/pix", settledAt)
	if err != nil {
		t.Fatalf("PutWebhook: %v", err)
	}
	if !created {
		t.Error("created = false on the first registration")
	}

	later := settledAt.Add(time.Hour)
	replaced, created, err := st.PutWebhook(ctx, "dev@example.com", "https://example.com/other", later)
	if err != nil {
		t.Fatalf("PutWebhook: %v", err)
	}
	if created {
		t.Error("created = true on a replacement")
	}
	if !replaced.CreatedAt.Equal(hook.CreatedAt) {
		t.Errorf("criacao = %s, want the original %s", replaced.CreatedAt, hook.CreatedAt)
	}

	read, err := st.GetWebhook(ctx, "dev@example.com")
	if err != nil {
		t.Fatalf("GetWebhook: %v", err)
	}
	if read.URL != "https://example.com/other" {
		t.Errorf("url = %q, want the replacement", read.URL)
	}

	events, err := st.EventsByAggregate(ctx, store.WebhookAggregate("dev@example.com"))
	if err != nil {
		t.Fatalf("EventsByAggregate: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("events = %d, want one per registration", len(events))
	}
}

func TestGetWebhookNotFound(t *testing.T) {
	st := newStore(t)

	if _, err := st.GetWebhook(context.Background(), "nobody@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
