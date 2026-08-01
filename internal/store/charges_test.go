package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/arinellidu/pix-sandbox-pay/internal/core"
	"github.com/arinellidu/pix-sandbox-pay/internal/store"
)

const sampleTxID = "abc123def456ghi789jkl012mno345"

func sampleCharge(txid string, created time.Time) core.Charge {
	return core.Charge{
		TxID:               txid,
		Status:             core.StatusAtiva,
		AmountCents:        1000,
		Chave:              "dev@example.com",
		SolicitacaoPagador: "Serviço realizado.",
		Devedor:            &core.Devedor{Nome: "Francisco da Silva", CPF: "12345678909"},
		Expiracao:          3600,
		EMV:                "00020101021226...",
		CreatedAt:          created,
		ExpiresAt:          created.Add(3600 * time.Second),
	}
}

func TestCreateChargeRoundTrip(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	created := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	want := sampleCharge(sampleTxID, created)
	got, wasCreated, err := st.CreateCharge(ctx, want)
	if err != nil {
		t.Fatalf("CreateCharge: %v", err)
	}
	if !wasCreated {
		t.Fatal("created = false on first insert")
	}
	if got.LocID != 1 {
		t.Errorf("loc id = %d, want 1", got.LocID)
	}

	read, err := st.GetCharge(ctx, sampleTxID)
	if err != nil {
		t.Fatalf("GetCharge: %v", err)
	}
	if read.AmountCents != 1000 || read.Chave != "dev@example.com" || read.Status != core.StatusAtiva {
		t.Errorf("charge = %+v", read)
	}
	if read.SolicitacaoPagador != want.SolicitacaoPagador {
		t.Errorf("solicitacaoPagador = %q, want %q", read.SolicitacaoPagador, want.SolicitacaoPagador)
	}
	if read.Devedor == nil || read.Devedor.CPF != "12345678909" || read.Devedor.Nome != "Francisco da Silva" {
		t.Errorf("devedor = %+v", read.Devedor)
	}
	if !read.CreatedAt.Equal(created) {
		t.Errorf("created_at = %s, want %s", read.CreatedAt, created)
	}
	if !read.ExpiresAt.Equal(created.Add(time.Hour)) {
		t.Errorf("expires_at = %s, want %s", read.ExpiresAt, created.Add(time.Hour))
	}
	if read.Expiracao != 3600 {
		t.Errorf("expiracao = %d, want 3600", read.Expiracao)
	}
}

func TestCreateChargeWithoutDevedor(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	charge := sampleCharge(sampleTxID, time.Now().UTC())
	charge.Devedor = nil
	if _, _, err := st.CreateCharge(ctx, charge); err != nil {
		t.Fatalf("CreateCharge: %v", err)
	}

	read, err := st.GetCharge(ctx, sampleTxID)
	if err != nil {
		t.Fatalf("GetCharge: %v", err)
	}
	if read.Devedor != nil {
		t.Errorf("devedor = %+v, want nil", read.Devedor)
	}
}

// INV-2: a replayed create returns the original charge and writes nothing.
func TestCreateChargeIsIdempotent(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	created := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	first, wasCreated, err := st.CreateCharge(ctx, sampleCharge(sampleTxID, created))
	if err != nil {
		t.Fatalf("first CreateCharge: %v", err)
	}
	if !wasCreated {
		t.Fatal("first create reported created = false")
	}

	// Replay with different content under the same txid.
	replay := sampleCharge(sampleTxID, created.Add(time.Hour))
	replay.AmountCents = 999999
	replay.Chave = "other@example.com"

	second, wasCreated, err := st.CreateCharge(ctx, replay)
	if err != nil {
		t.Fatalf("replay CreateCharge: %v", err)
	}
	if wasCreated {
		t.Error("replay reported created = true")
	}
	if second.AmountCents != first.AmountCents || second.Chave != first.Chave {
		t.Errorf("replay returned the new charge %+v, want the original %+v", second, first)
	}

	var rows int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM charges`).Scan(&rows); err != nil {
		t.Fatalf("count charges: %v", err)
	}
	if rows != 1 {
		t.Errorf("charges rows = %d, want 1", rows)
	}

	events, err := st.EventsByAggregate(ctx, store.ChargeAggregate(sampleTxID))
	if err != nil {
		t.Fatalf("EventsByAggregate: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1; a replay must not log a second creation", len(events))
	}
	if events[0].Type != store.EventChargeCreated {
		t.Errorf("event type = %q, want %q", events[0].Type, store.EventChargeCreated)
	}
}

// INV-3: the creation is in the log, with the domain's own units.
func TestCreateChargeLogsEvent(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	if _, _, err := st.CreateCharge(ctx, sampleCharge(sampleTxID, time.Now().UTC())); err != nil {
		t.Fatalf("CreateCharge: %v", err)
	}

	events, err := st.EventsByAggregate(ctx, store.ChargeAggregate(sampleTxID))
	if err != nil {
		t.Fatalf("EventsByAggregate: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	var payload struct {
		TxID        string `json:"txid"`
		Status      string `json:"status"`
		AmountCents int64  `json:"amount_cents"`
		Devedor     struct {
			Nome string `json:"nome"`
			CPF  string `json:"cpf"`
		} `json:"devedor"`
	}
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.TxID != sampleTxID || payload.Status != "ATIVA" || payload.AmountCents != 1000 {
		t.Errorf("payload = %+v", payload)
	}
	if payload.Devedor.CPF != "12345678909" {
		t.Errorf("devedor.cpf = %q", payload.Devedor.CPF)
	}
}

func TestGetChargeNotFound(t *testing.T) {
	st := newStore(t)

	_, err := st.GetCharge(context.Background(), "nosuchtxid1234567890123456")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetCharge(missing) = %v, want ErrNotFound", err)
	}
}

func TestLocIDIncrements(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i, txid := range []string{
		"aaa123def456ghi789jkl012mno345",
		"bbb123def456ghi789jkl012mno345",
		"ccc123def456ghi789jkl012mno345",
	} {
		got, _, err := st.CreateCharge(ctx, sampleCharge(txid, now))
		if err != nil {
			t.Fatalf("CreateCharge %d: %v", i, err)
		}
		if want := int64(i + 1); got.LocID != want {
			t.Errorf("loc id = %d, want %d", got.LocID, want)
		}
	}
}

func TestExpireCharge(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	created := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	if _, _, err := st.CreateCharge(ctx, sampleCharge(sampleTxID, created)); err != nil {
		t.Fatalf("CreateCharge: %v", err)
	}

	// Still inside the window: nothing changes, nothing is logged.
	charge, err := st.ExpireCharge(ctx, sampleTxID, created.Add(time.Minute))
	if err != nil {
		t.Fatalf("ExpireCharge inside window: %v", err)
	}
	if charge.Status != core.StatusAtiva {
		t.Errorf("status = %q, want ATIVA", charge.Status)
	}

	// Past the window: the transition happens and lands in the log.
	charge, err = st.ExpireCharge(ctx, sampleTxID, created.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ExpireCharge past window: %v", err)
	}
	if charge.Status != core.StatusExpirada {
		t.Errorf("status = %q, want EXPIRADA", charge.Status)
	}

	read, err := st.GetCharge(ctx, sampleTxID)
	if err != nil {
		t.Fatalf("GetCharge: %v", err)
	}
	if read.Status != core.StatusExpirada {
		t.Errorf("persisted status = %q, want EXPIRADA", read.Status)
	}

	// Calling again must not log the transition twice.
	if _, err := st.ExpireCharge(ctx, sampleTxID, created.Add(3*time.Hour)); err != nil {
		t.Fatalf("second ExpireCharge: %v", err)
	}

	events, err := st.EventsByAggregate(ctx, store.ChargeAggregate(sampleTxID))
	if err != nil {
		t.Fatalf("EventsByAggregate: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (created, expired)", len(events))
	}
	if events[1].Type != store.EventChargeExpired {
		t.Errorf("second event = %q, want %q", events[1].Type, store.EventChargeExpired)
	}
}

func TestExpireChargeLeavesSettledAlone(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	created := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	charge := sampleCharge(sampleTxID, created)
	charge.Status = core.StatusConcluida
	if _, _, err := st.CreateCharge(ctx, charge); err != nil {
		t.Fatalf("CreateCharge: %v", err)
	}

	got, err := st.ExpireCharge(ctx, sampleTxID, created.Add(10*time.Hour))
	if err != nil {
		t.Fatalf("ExpireCharge: %v", err)
	}
	if got.Status != core.StatusConcluida {
		t.Errorf("status = %q, want CONCLUIDA: a settled charge never expires", got.Status)
	}
}

func TestSchemaVersionAdvances(t *testing.T) {
	st := newStore(t)

	var version int
	if err := st.DB().QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version < 2 {
		t.Errorf("user_version = %d, want at least 2 migrations applied", version)
	}
}

// s0Schema is the database S0 left behind: the tables of migration 0001, no
// version marker, and none of the columns S1 needs. Written out by hand so the
// fixture stays a real legacy database rather than a doctored current one.
const s0Schema = `
CREATE TABLE events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    aggregate  TEXT    NOT NULL,
    type       TEXT    NOT NULL,
    payload    TEXT    NOT NULL DEFAULT '{}',
    created_at TEXT    NOT NULL
);
CREATE TABLE charges (
    txid         TEXT    PRIMARY KEY,
    status       TEXT    NOT NULL,
    amount_cents INTEGER NOT NULL,
    chave        TEXT    NOT NULL,
    emv          TEXT    NOT NULL DEFAULT '',
    created_at   TEXT    NOT NULL,
    expires_at   TEXT    NOT NULL
);
INSERT INTO events (aggregate, type, payload, created_at)
VALUES ('oauth', 'oauth.token.issued', '{}', '2026-07-31T12:00:00Z');
`

// A database written by S0 must migrate forward, keeping what it already holds.
func TestMigrateFromS0Database(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sandbox.db")

	legacy, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	if _, err := legacy.Exec(s0Schema); err != nil {
		t.Fatalf("write S0 schema: %v", err)
	}
	var version int
	if err := legacy.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read version: %v", err)
	}
	if version != 0 {
		t.Fatalf("fixture user_version = %d, want 0", version)
	}
	legacy.Close()

	migrated, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer migrated.Close()

	if _, _, err := migrated.CreateCharge(context.Background(), sampleCharge(sampleTxID, time.Now().UTC())); err != nil {
		t.Fatalf("CreateCharge after migration: %v", err)
	}
	events, err := migrated.EventsByAggregate(context.Background(), "oauth")
	if err != nil {
		t.Fatalf("EventsByAggregate: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("pre-existing events = %d, want 1 preserved across migration", len(events))
	}
}

// A database from a newer binary must not be silently downgraded.
func TestOpenRejectsFutureSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "sandbox.db")

	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := st.DB().Exec(`PRAGMA user_version = 999`); err != nil {
		t.Fatalf("set future version: %v", err)
	}
	st.Close()

	if _, err := store.Open(path); err == nil {
		t.Error("Open on a future schema = nil error, want refusal")
	}
}
