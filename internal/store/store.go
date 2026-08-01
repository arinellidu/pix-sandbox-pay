// Package store owns persistence: the append-only event log and the
// projections derived from it. SQLite is embedded via modernc.org/sqlite so
// the binary stays CGO-free and dependency-free at runtime (ADR-002).
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a lookup finds no row.
var ErrNotFound = errors.New("store: not found")

// Store is a handle on the sandbox database.
type Store struct {
	db *sql.DB
}

// Event is one entry of the append-only log.
type Event struct {
	ID        int64           `json:"id"`
	Aggregate string          `json:"aggregate"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// Open opens (creating it if needed) the SQLite database at path and brings it
// up to the current schema. Parent directories are created as needed.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite takes a single writer; one connection keeps writes serialized and
	// avoids SQLITE_BUSY under concurrent handlers.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func dsn(path string) string {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	return "file:" + path + "?" + q.Encode()
}

// DB exposes the underlying handle for packages that need direct SQL.
func (s *Store) DB() *sql.DB { return s.db }

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// Ping verifies the database is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// AppendEvent writes one entry to the append-only log and returns its id.
// payload may be nil, any JSON-marshalable value, or a json.RawMessage.
func (s *Store) AppendEvent(ctx context.Context, aggregate, typ string, payload any) (int64, error) {
	raw, err := marshalPayload(payload)
	if err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, insertEventSQL,
		aggregate, typ, raw, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("append event: %w", err)
	}
	return res.LastInsertId()
}

const insertEventSQL = `INSERT INTO events (aggregate, type, payload, created_at) VALUES (?, ?, ?, ?)`

// appendEventTx logs an event inside a caller's transaction, so a state change
// and its event commit together or not at all (INV-3).
func appendEventTx(ctx context.Context, tx *sql.Tx, aggregate, typ string, payload any) error {
	raw, err := marshalPayload(payload)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, insertEventSQL,
		aggregate, typ, raw, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("append event %s: %w", typ, err)
	}
	return nil
}

func marshalPayload(payload any) (string, error) {
	if payload == nil {
		return "{}", nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal event payload: %w", err)
	}
	return string(b), nil
}

// EventsByAggregate returns the log for one aggregate, oldest first.
func (s *Store) EventsByAggregate(ctx context.Context, aggregate string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, aggregate, type, payload, created_at FROM events WHERE aggregate = ? ORDER BY id`,
		aggregate,
	)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]Event, error) {
	var out []Event
	for rows.Next() {
		var (
			e       Event
			payload string
			created string
			err     error
		)
		if err := rows.Scan(&e.ID, &e.Aggregate, &e.Type, &payload, &created); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Payload = json.RawMessage(payload)
		if e.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, fmt.Errorf("parse event timestamp: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
