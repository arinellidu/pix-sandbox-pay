package webhook_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/arinellidu/pix-sandbox-pay/internal/webhook"
)

// recorder captures what the dispatcher would have written to the event log.
type recorder struct {
	mu     sync.Mutex
	events []recorded
	done   chan struct{}
	once   sync.Once
}

type recorded struct {
	aggregate string
	typ       string
	payload   any
}

func newRecorder() *recorder { return &recorder{done: make(chan struct{})} }

func (r *recorder) AppendEvent(_ context.Context, aggregate, typ string, payload any) (int64, error) {
	r.mu.Lock()
	r.events = append(r.events, recorded{aggregate, typ, payload})
	n := int64(len(r.events))
	r.mu.Unlock()
	r.once.Do(func() { close(r.done) })
	return n, nil
}

// wait blocks until the dispatcher has recorded its verdict.
func (r *recorder) wait(t *testing.T) recorded {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		t.Fatal("no outcome was recorded")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.events[0]
}

func attemptsOf(t *testing.T, rec recorded) []map[string]any {
	t.Helper()
	payload, ok := rec.payload.(map[string]any)
	if !ok {
		t.Fatalf("payload = %T, want a map", rec.payload)
	}
	// The payload travels as JSON, so read it back the way a log reader would.
	raw, err := json.Marshal(payload["attempts"])
	if err != nil {
		t.Fatalf("marshal attempts: %v", err)
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal attempts: %v", err)
	}
	return out
}

// closeAfter drains the dispatcher so no delivery outlives the test.
func closeAfter(t *testing.T, d *webhook.Dispatcher) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := d.Close(ctx); err != nil {
			t.Errorf("close dispatcher: %v", err)
		}
	})
}

func TestDeliverSignsTheBody(t *testing.T) {
	type call struct {
		body      []byte
		signature string
		mediaType string
	}
	calls := make(chan call, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls <- call{body, r.Header.Get(webhook.SignatureHeader), r.Header.Get("Content-Type")}
	}))
	defer server.Close()

	rec := newRecorder()
	d := webhook.New(rec, webhook.Config{Secret: "shh"})
	closeAfter(t, d)

	d.Deliver("pix:E1", server.URL, map[string]any{"pix": []string{"one"}})

	var got call
	select {
	case got = <-calls:
	case <-time.After(5 * time.Second):
		t.Fatal("the endpoint was never called")
	}

	if want := webhook.Sign("shh", got.body); got.signature != want {
		t.Errorf("%s = %q, want %q", webhook.SignatureHeader, got.signature, want)
	}
	if got.mediaType != "application/json" {
		t.Errorf("content-type = %q, want application/json", got.mediaType)
	}
	if string(got.body) != `{"pix":["one"]}` {
		t.Errorf("body = %s", got.body)
	}

	outcome := rec.wait(t)
	if outcome.typ != webhook.EventDelivered {
		t.Errorf("event = %q, want %q", outcome.typ, webhook.EventDelivered)
	}
	if outcome.aggregate != "pix:E1" {
		t.Errorf("aggregate = %q, want the payment's", outcome.aggregate)
	}
}

// The signature covers the exact bytes sent, so a receiver can verify before
// parsing — and a tampered body fails.
func TestSignatureIsOverTheExactBytes(t *testing.T) {
	body := []byte(`{"pix":[]}`)
	if webhook.Sign("shh", body) == webhook.Sign("shh", append(body, ' ')) {
		t.Error("a modified body produced the same signature")
	}
	if webhook.Sign("shh", body) == webhook.Sign("other", body) {
		t.Error("a different secret produced the same signature")
	}
}

func TestDeliverRetriesUntilTheEndpointRecovers(t *testing.T) {
	var mu sync.Mutex
	var hits int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		n := hits
		mu.Unlock()
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	rec := newRecorder()
	d := webhook.New(rec, webhook.Config{
		Backoff: []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond},
	})
	closeAfter(t, d)

	d.Deliver("pix:E1", server.URL, map[string]any{})

	outcome := rec.wait(t)
	if outcome.typ != webhook.EventDelivered {
		t.Fatalf("event = %q, want %q", outcome.typ, webhook.EventDelivered)
	}
	if attempts := attemptsOf(t, outcome); len(attempts) != 3 {
		t.Errorf("attempts = %d, want 3 (two 500s then a 200)", len(attempts))
	}
}

func TestDeliverGivesUpAfterTheSchedule(t *testing.T) {
	var mu sync.Mutex
	var hits int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	rec := newRecorder()
	d := webhook.New(rec, webhook.Config{
		Backoff: []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond},
	})
	closeAfter(t, d)

	d.Deliver("pix:E1", server.URL, map[string]any{})

	outcome := rec.wait(t)
	if outcome.typ != webhook.EventFailed {
		t.Fatalf("event = %q, want %q", outcome.typ, webhook.EventFailed)
	}
	if attempts := attemptsOf(t, outcome); len(attempts) != 4 {
		t.Errorf("attempts = %d, want 4 (the first plus three retries)", len(attempts))
	}

	mu.Lock()
	defer mu.Unlock()
	if hits != 4 {
		t.Errorf("the endpoint saw %d requests, want 4", hits)
	}
}

// A 4xx is the receiver saying the request itself is wrong. Repeating it
// verbatim would only waste both sides' time.
func TestDeliverDoesNotRetryClientErrors(t *testing.T) {
	var mu sync.Mutex
	var hits int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	rec := newRecorder()
	d := webhook.New(rec, webhook.Config{Backoff: []time.Duration{time.Millisecond}})
	closeAfter(t, d)

	d.Deliver("pix:E1", server.URL, map[string]any{})

	outcome := rec.wait(t)
	if outcome.typ != webhook.EventFailed {
		t.Fatalf("event = %q, want %q", outcome.typ, webhook.EventFailed)
	}

	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Errorf("the endpoint saw %d requests, want 1", hits)
	}
}

// An unreachable endpoint is recorded rather than lost, and it never blocks
// the caller: Deliver returns before the schedule has run.
func TestDeliverRecordsTransportFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	rec := newRecorder()
	d := webhook.New(rec, webhook.Config{Backoff: []time.Duration{}})
	closeAfter(t, d)

	d.Deliver("pix:E1", url, map[string]any{})

	outcome := rec.wait(t)
	if outcome.typ != webhook.EventFailed {
		t.Fatalf("event = %q, want %q", outcome.typ, webhook.EventFailed)
	}
	attempts := attemptsOf(t, outcome)
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(attempts))
	}
	if attempts[0]["error"] == "" {
		t.Error("the attempt recorded no error")
	}
}

// Close cuts a long backoff short instead of holding shutdown open for the
// whole schedule.
func TestCloseInterruptsPendingRetries(t *testing.T) {
	hit := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case hit <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	rec := newRecorder()
	d := webhook.New(rec, webhook.Config{Backoff: []time.Duration{time.Hour}})
	d.Deliver("pix:E1", server.URL, map[string]any{})

	select {
	case <-hit:
	case <-time.After(5 * time.Second):
		t.Fatal("the first attempt never landed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	outcome := rec.wait(t)
	if outcome.typ != webhook.EventFailed {
		t.Errorf("event = %q, want %q", outcome.typ, webhook.EventFailed)
	}
}

// A request already on the wire when Close arrives is drained, not aborted:
// the payee receives the callback and the log says delivered, not failed.
func TestCloseDrainsInFlightDelivery(t *testing.T) {
	received := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(received)
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	rec := newRecorder()
	d := webhook.New(rec, webhook.Config{})
	d.Deliver("pix:E1", server.URL, map[string]any{})

	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("the attempt never landed")
	}

	// Let the endpoint answer only after Close has started waiting.
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(release)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	outcome := rec.wait(t)
	if outcome.typ != webhook.EventDelivered {
		t.Errorf("event = %q, want %q — shutdown aborted a delivery in flight", outcome.typ, webhook.EventDelivered)
	}
}

// A closed dispatcher drops new work rather than starting it.
func TestDeliverAfterCloseIsDropped(t *testing.T) {
	hit := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit <- struct{}{}
	}))
	defer server.Close()

	rec := newRecorder()
	d := webhook.New(rec, webhook.Config{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	d.Deliver("pix:E1", server.URL, map[string]any{})

	select {
	case <-hit:
		t.Error("the endpoint was called after Close")
	case <-time.After(100 * time.Millisecond):
	}
}
