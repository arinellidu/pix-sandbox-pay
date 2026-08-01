-- S2 adds the settlement side of the lifecycle: the pix a charge settles
-- into, the refunds raised against it, and the callback endpoint a payee
-- registered for its key.

CREATE TABLE IF NOT EXISTS payments (
    e2e_id         TEXT    PRIMARY KEY,
    seq            INTEGER NOT NULL UNIQUE,
    -- One payment per charge: an immediate cob settles exactly once, so a
    -- double settlement is refused by the database, not only by the engine.
    txid           TEXT    NOT NULL UNIQUE REFERENCES charges (txid),
    chave          TEXT    NOT NULL,
    status         TEXT    NOT NULL,
    amount_cents   INTEGER NOT NULL,
    refunded_cents INTEGER NOT NULL DEFAULT 0,
    info_pagador   TEXT    NOT NULL DEFAULT '',
    created_at     TEXT    NOT NULL, -- RFC3339, UTC
    -- INV-4, enforced by the database: refunds never exceed what settled.
    CHECK (refunded_cents >= 0 AND refunded_cents <= amount_cents)
);

CREATE INDEX IF NOT EXISTS idx_payments_created ON payments (created_at);

CREATE TABLE IF NOT EXISTS refunds (
    id           TEXT    NOT NULL,
    e2e_id       TEXT    NOT NULL REFERENCES payments (e2e_id),
    rtr_id       TEXT    NOT NULL UNIQUE,
    seq          INTEGER NOT NULL UNIQUE,
    amount_cents INTEGER NOT NULL CHECK (amount_cents > 0),
    status       TEXT    NOT NULL,
    motivo       TEXT    NOT NULL DEFAULT '',
    created_at   TEXT    NOT NULL, -- RFC3339, UTC
    settled_at   TEXT    NOT NULL DEFAULT '',
    -- The refund id is chosen by the payee and unique per payment, which is
    -- what makes PUT .../devolucao/{id} idempotent (INV-2).
    PRIMARY KEY (e2e_id, id)
);

CREATE TABLE IF NOT EXISTS webhooks (
    chave      TEXT PRIMARY KEY,
    url        TEXT NOT NULL,
    created_at TEXT NOT NULL, -- RFC3339, UTC
    updated_at TEXT NOT NULL
);
