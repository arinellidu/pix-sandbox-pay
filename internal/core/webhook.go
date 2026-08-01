package core

import (
	"fmt"
	"net/url"
	"time"
)

// MaxWebhookURLLen caps the registered callback URL.
const MaxWebhookURLLen = 500

// Webhook is the callback endpoint a payee registered for one of its keys.
type Webhook struct {
	Chave     string
	URL       string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ValidateWebhookURL checks the shape of a callback URL.
//
// BACEN requires https; the sandbox also accepts http, because the endpoint
// under test is usually an echo server on localhost and demanding TLS there
// would buy nothing — no real money and no real payer data ever crosses it.
func ValidateWebhookURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("webhookUrl is required")
	}
	if len(raw) > MaxWebhookURLLen {
		return fmt.Errorf("webhookUrl must be at most %d characters, got %d", MaxWebhookURLLen, len(raw))
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("webhookUrl is not a valid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webhookUrl must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("webhookUrl must include a host")
	}
	return nil
}
