package store_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/arinellidu/pix-sandbox-pay/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	// A nested dir also asserts Open creates the data directory.
	st, err := store.Open(filepath.Join(t.TempDir(), "data", "sandbox.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestOpenCreatesSchema(t *testing.T) {
	st := newStore(t)

	for _, table := range []string{"events", "charges"} {
		var name string
		err := st.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("table %q not created: %v", table, err)
		}
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "sandbox.db")

	first, err := store.Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if _, err := first.AppendEvent(context.Background(), "cob:abc", "cob.created", nil); err != nil {
		t.Fatalf("append: %v", err)
	}
	first.Close()

	second, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()

	events, err := second.EventsByAggregate(context.Background(), "cob:abc")
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events after reopen = %d, want 1", len(events))
	}
}

func TestAppendAndReadEvents(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	if _, err := st.AppendEvent(ctx, "cob:tx1", "cob.created", map[string]any{"amount_cents": 1000}); err != nil {
		t.Fatalf("append first: %v", err)
	}
	if _, err := st.AppendEvent(ctx, "cob:tx1", "cob.paid", nil); err != nil {
		t.Fatalf("append second: %v", err)
	}
	if _, err := st.AppendEvent(ctx, "cob:tx2", "cob.created", nil); err != nil {
		t.Fatalf("append other aggregate: %v", err)
	}

	events, err := st.EventsByAggregate(ctx, "cob:tx1")
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Type != "cob.created" || events[1].Type != "cob.paid" {
		t.Fatalf("events out of order: %s, %s", events[0].Type, events[1].Type)
	}
	if events[0].ID >= events[1].ID {
		t.Fatalf("ids not increasing: %d, %d", events[0].ID, events[1].ID)
	}
	if events[0].CreatedAt.IsZero() {
		t.Error("created_at not populated")
	}

	var payload struct {
		AmountCents int64 `json:"amount_cents"`
	}
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if payload.AmountCents != 1000 {
		t.Errorf("amount_cents = %d, want 1000", payload.AmountCents)
	}
	if string(events[1].Payload) != "{}" {
		t.Errorf("nil payload = %s, want {}", events[1].Payload)
	}
}

// INV-3 leans on the log being append-only; the database enforces it.
func TestEventLogIsAppendOnly(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	if _, err := st.AppendEvent(ctx, "cob:tx1", "cob.created", nil); err != nil {
		t.Fatalf("append: %v", err)
	}

	if _, err := st.DB().Exec(`UPDATE events SET type = 'tampered' WHERE id = 1`); err == nil {
		t.Error("UPDATE on events succeeded, want rejected")
	}
	if _, err := st.DB().Exec(`DELETE FROM events WHERE id = 1`); err == nil {
		t.Error("DELETE on events succeeded, want rejected")
	}

	events, err := st.EventsByAggregate(ctx, "cob:tx1")
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 1 || events[0].Type != "cob.created" {
		t.Fatalf("log altered: %+v", events)
	}
}

func TestChargesTableRoundTrip(t *testing.T) {
	st := newStore(t)

	const insert = `INSERT INTO charges (txid, status, amount_cents, chave, emv, created_at, expires_at)
	                VALUES (?, ?, ?, ?, ?, ?, ?)`
	if _, err := st.DB().Exec(insert,
		"tx1", "ATIVA", int64(1000), "dev@example.com", "", "2026-07-31T12:00:00Z", "2026-07-31T13:00:00Z",
	); err != nil {
		t.Fatalf("insert charge: %v", err)
	}

	var (
		status string
		cents  int64
		chave  string
	)
	if err := st.DB().QueryRow(
		`SELECT status, amount_cents, chave FROM charges WHERE txid = ?`, "tx1",
	).Scan(&status, &cents, &chave); err != nil {
		t.Fatalf("read charge: %v", err)
	}
	if status != "ATIVA" || cents != 1000 || chave != "dev@example.com" {
		t.Fatalf("charge = (%s, %d, %s)", status, cents, chave)
	}

	// INV-2: txid is unique.
	if _, err := st.DB().Exec(insert,
		"tx1", "ATIVA", int64(2000), "other@example.com", "", "2026-07-31T12:00:00Z", "2026-07-31T13:00:00Z",
	); err == nil {
		t.Error("duplicate txid accepted, want primary-key violation")
	}
}
