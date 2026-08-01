// Package webhook delivers callbacks to the endpoint a payee registered.
//
// Delivery is asynchronous, signed and retried, and every outcome lands in the
// event log — a failed callback is part of the story a payment tells, and the
// console timeline of a payment should show it. The failure *modes* (dropped
// deliveries, forced 500s) arrive with the chaos API; this package only does
// the honest thing well.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// SignatureHeader carries the HMAC-SHA256 of the exact body bytes, hex-encoded.
const SignatureHeader = "X-Signature"

// DefaultSecret signs deliveries when WEBHOOK_SECRET is unset.
//
// It is deliberately a published constant, not a generated one: the sandbox
// must verify out of the box, and nothing it signs is worth forging. Override
// it through the environment when the receiver under test checks signatures
// against its own secret.
const DefaultSecret = "pix-sandbox"

// DefaultBackoff is the retry schedule: three retries after the first attempt.
var DefaultBackoff = []time.Duration{1 * time.Second, 5 * time.Second, 25 * time.Second}

// defaultTimeout bounds a single attempt.
const defaultTimeout = 10 * time.Second

// Recorder is the slice of the store the dispatcher needs: somewhere to write
// what happened. *store.Store satisfies it.
type Recorder interface {
	AppendEvent(ctx context.Context, aggregate, typ string, payload any) (int64, error)
}

// Event types the dispatcher writes.
const (
	EventDelivered = "webhook.delivered"
	EventFailed    = "webhook.failed"
)

// Config tunes the dispatcher. The zero value is usable.
type Config struct {
	// Secret signs the body. Empty means DefaultSecret.
	Secret string
	// Backoff is the wait before each retry. Nil means DefaultBackoff; an
	// empty non-nil slice disables retries.
	Backoff []time.Duration
	// Client sends the requests. Nil means one with defaultTimeout.
	Client *http.Client
	// Log receives delivery failures. Nil discards them.
	Log *slog.Logger
}

// Dispatcher posts signed callbacks in the background.
type Dispatcher struct {
	rec     Recorder
	secret  []byte
	backoff []time.Duration
	client  *http.Client
	log     *slog.Logger

	// closing is closed first on Close: pending backoffs stop waiting, but a
	// request already on the wire is left to finish inside Close's budget.
	// ctx is cancelled only when that budget runs out (or after the drain),
	// aborting whatever is still in flight.
	closing chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc

	wg     sync.WaitGroup
	mu     sync.Mutex
	closed bool
}

// New builds a Dispatcher.
func New(rec Recorder, cfg Config) *Dispatcher {
	secret := cfg.Secret
	if secret == "" {
		secret = DefaultSecret
	}
	backoff := cfg.Backoff
	if backoff == nil {
		backoff = DefaultBackoff
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Dispatcher{
		rec: rec, secret: []byte(secret), backoff: backoff,
		client: client, log: log,
		closing: make(chan struct{}), ctx: ctx, cancel: cancel,
	}
}

// Deliver posts payload to url in the background and returns immediately: an
// API response never waits on the payee's endpoint. The outcome is written to
// the log under aggregate, so it shows up on that payment's timeline.
func (d *Dispatcher) Deliver(aggregate, url string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		d.log.Error("marshal webhook payload", "url", url, "err", err)
		return
	}

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		d.log.Warn("webhook dropped: dispatcher is closed", "url", url)
		return
	}
	d.wg.Add(1)
	d.mu.Unlock()

	go func() {
		defer d.wg.Done()
		d.deliver(aggregate, url, body)
	}()
}

// attempt is one delivery try, as it appears in the log.
type attempt struct {
	N          int    `json:"n"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

func (d *Dispatcher) deliver(aggregate, url string, body []byte) {
	signature := sign(d.secret, body)
	attempts := make([]attempt, 0, len(d.backoff)+1)

	for n := 0; ; n++ {
		if n > 0 && !d.wait(d.backoff[n-1]) {
			break
		}

		start := time.Now()
		status, err := d.post(url, body, signature)
		a := attempt{N: n + 1, StatusCode: status, DurationMS: time.Since(start).Milliseconds()}
		if err != nil {
			a.Error = err.Error()
		}
		attempts = append(attempts, a)

		if err == nil && status < http.StatusBadRequest {
			d.record(aggregate, EventDelivered, url, attempts)
			return
		}
		if !retryable(status, err) || n == len(d.backoff) {
			break
		}
		d.log.Warn("webhook delivery failed, retrying",
			"url", url, "attempt", a.N, "status", status, "err", err)
	}

	d.log.Error("webhook delivery gave up", "url", url, "attempts", len(attempts))
	d.record(aggregate, EventFailed, url, attempts)
}

func (d *Dispatcher) post(url string, body, signature []byte) (int, error) {
	req, err := http.NewRequestWithContext(d.ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "pix-sandbox")
	req.Header.Set(SignatureHeader, string(signature))

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// The body is drained and dropped: the receiver's answer is not part of
	// the protocol, but reading it lets the connection be reused.
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))

	if resp.StatusCode >= http.StatusBadRequest {
		return resp.StatusCode, fmt.Errorf("endpoint answered %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// retryable decides whether another attempt could plausibly do better. A 4xx
// other than 429 is the receiver saying the request itself is wrong; repeating
// it verbatim only wastes both sides' time.
func retryable(status int, err error) bool {
	switch {
	case status == 0:
		return err != nil // transport failure: worth another try
	case status == http.StatusTooManyRequests:
		return true
	case status >= http.StatusInternalServerError:
		return true
	default:
		return false
	}
}

// wait sleeps for d, reporting false if the dispatcher closed meanwhile: a
// retry that has not started is not worth holding shutdown open for.
func (d *Dispatcher) wait(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-d.closing:
		return false
	case <-d.ctx.Done():
		return false
	}
}

func (d *Dispatcher) record(aggregate, typ, url string, attempts []attempt) {
	// A cancelled dispatcher still records its verdict: use a fresh context so
	// shutdown does not swallow the last event.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	payload := map[string]any{
		"webhook_url": url,
		"attempts":    attempts,
	}
	if last := attempts[len(attempts)-1]; last.StatusCode != 0 {
		payload["status_code"] = last.StatusCode
	}
	if _, err := d.rec.AppendEvent(ctx, aggregate, typ, payload); err != nil {
		d.log.Error("record webhook outcome", "type", typ, "url", url, "err", err)
	}
}

// Close stops accepting deliveries and pending retries, then waits for the
// requests already on the wire to finish recording, bounded by ctx. Only when
// that budget runs out are they aborted — cancelling first would turn every
// callback that happened to straddle shutdown into a webhook.failed the payee
// actually received. It returns ctx's error when the budget cut the drain.
func (d *Dispatcher) Close(ctx context.Context) error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	d.mu.Unlock()

	close(d.closing)

	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		d.cancel()
		return nil
	case <-ctx.Done():
		d.cancel()
		return ctx.Err()
	}
}

// sign is HMAC-SHA256 over the exact bytes sent, hex-encoded. A receiver
// verifies it over the raw body, before parsing it.
func sign(secret, body []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sum := mac.Sum(nil)
	out := make([]byte, hex.EncodedLen(len(sum)))
	hex.Encode(out, sum)
	return out
}

// Sign exposes the signature scheme so tests — and readers wiring a receiver —
// can reproduce it.
func Sign(secret string, body []byte) string { return string(sign([]byte(secret), body)) }
