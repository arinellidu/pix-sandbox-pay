package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/arinelliquebec/pix-sandbox/internal/core"
)

// Event types written by webhook registration and delivery. The delivery ones
// are emitted by the dispatcher, which names them itself; they are declared
// here so readers of the log have one place to look.
const (
	EventWebhookRegistered = "webhook.registered"
	EventWebhookDelivered  = "webhook.delivered"
	EventWebhookFailed     = "webhook.failed"
)

// WebhookAggregate is the event-log aggregate id for a registration.
func WebhookAggregate(chave string) string { return "webhook:" + chave }

// PutWebhook registers (or replaces) the callback endpoint for a key and logs
// webhook.registered. Replacing keeps the original creation instant, as the
// API Pix reports `criacao` for the registration rather than the last edit.
func (s *Store) PutWebhook(ctx context.Context, chave, rawURL string, now time.Time) (core.Webhook, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.Webhook{}, false, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	hook := core.Webhook{Chave: chave, URL: rawURL, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}

	existing, err := scanWebhook(tx.QueryRowContext(ctx,
		`SELECT chave, url, created_at, updated_at FROM webhooks WHERE chave = ?`, chave))
	switch {
	case err == nil:
		hook.CreatedAt = existing.CreatedAt
	case !errors.Is(err, ErrNotFound):
		return core.Webhook{}, false, err
	}
	created := errors.Is(err, ErrNotFound)

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO webhooks (chave, url, created_at, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT (chave) DO UPDATE SET url = excluded.url, updated_at = excluded.updated_at`,
		hook.Chave, hook.URL, formatTime(hook.CreatedAt), formatTime(hook.UpdatedAt),
	); err != nil {
		return core.Webhook{}, false, fmt.Errorf("upsert webhook: %w", err)
	}

	if err := appendEventTx(ctx, tx, WebhookAggregate(chave), EventWebhookRegistered, map[string]any{
		"chave":       chave,
		"webhook_url": rawURL,
		"created":     created,
	}); err != nil {
		return core.Webhook{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return core.Webhook{}, false, fmt.Errorf("commit: %w", err)
	}
	return hook, created, nil
}

// GetWebhook returns the registration for a key, or ErrNotFound.
func (s *Store) GetWebhook(ctx context.Context, chave string) (core.Webhook, error) {
	return scanWebhook(s.db.QueryRowContext(ctx,
		`SELECT chave, url, created_at, updated_at FROM webhooks WHERE chave = ?`, chave))
}

func scanWebhook(row rowScanner) (core.Webhook, error) {
	var (
		w         core.Webhook
		createdAt string
		updatedAt string
	)
	err := row.Scan(&w.Chave, &w.URL, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return core.Webhook{}, ErrNotFound
	}
	if err != nil {
		return core.Webhook{}, fmt.Errorf("scan webhook: %w", err)
	}
	if w.CreatedAt, err = parseTime(createdAt); err != nil {
		return core.Webhook{}, err
	}
	if w.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return core.Webhook{}, err
	}
	return w, nil
}
