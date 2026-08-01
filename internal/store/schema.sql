-- Append-only event log: the source of truth (ADR-003). Every state
-- transition lands here; projections are derived from it (INV-3).
CREATE TABLE IF NOT EXISTS events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    aggregate  TEXT    NOT NULL,
    type       TEXT    NOT NULL,
    payload    TEXT    NOT NULL DEFAULT '{}', -- JSON
    created_at TEXT    NOT NULL               -- RFC3339, UTC
);

CREATE INDEX IF NOT EXISTS idx_events_aggregate ON events (aggregate, id);

-- Append-only is enforced by the database, not by convention.
CREATE TRIGGER IF NOT EXISTS events_no_update
BEFORE UPDATE ON events
BEGIN
    SELECT RAISE(ABORT, 'events is append-only');
END;

CREATE TRIGGER IF NOT EXISTS events_no_delete
BEFORE DELETE ON events
BEGIN
    SELECT RAISE(ABORT, 'events is append-only');
END;

-- Projection of the charge (cob) aggregate. Money is stored in cents.
CREATE TABLE IF NOT EXISTS charges (
    txid         TEXT    PRIMARY KEY,
    status       TEXT    NOT NULL,
    amount_cents INTEGER NOT NULL,
    chave        TEXT    NOT NULL,
    emv          TEXT    NOT NULL DEFAULT '',
    created_at   TEXT    NOT NULL, -- RFC3339, UTC
    expires_at   TEXT    NOT NULL  -- RFC3339, UTC
);

CREATE INDEX IF NOT EXISTS idx_charges_status ON charges (status, created_at);
