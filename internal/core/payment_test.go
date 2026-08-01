package core_test

import (
	"strings"
	"testing"
	"time"

	"github.com/arinellidu/pix-sandbox-pay/internal/core"
	"github.com/arinellidu/pix-sandbox-pay/internal/rng"
)

var mintedAt = time.Date(2026, 7, 31, 12, 4, 30, 0, time.UTC)

func TestNewE2EIDShape(t *testing.T) {
	src := rng.New(rng.DefaultSeed)

	id, err := core.NewE2EID(src, mintedAt, 1)
	if err != nil {
		t.Fatalf("NewE2EID: %v", err)
	}
	if len(id) != 32 {
		t.Fatalf("e2eid %q has length %d, want 32", id, len(id))
	}
	if got := id[:1]; got != "E" {
		t.Errorf("prefix = %q, want E", got)
	}
	if got := id[1:9]; got != core.SandboxISPB {
		t.Errorf("ispb = %q, want %q", got, core.SandboxISPB)
	}
	// Minute precision, UTC: seconds are not part of the identifier.
	if got := id[9:21]; got != "202607311204" {
		t.Errorf("timestamp = %q, want 202607311204", got)
	}
	if err := core.ValidateE2EID(id); err != nil {
		t.Errorf("minted id does not validate: %v", err)
	}
}

// The trailing sequence is what makes uniqueness structural (INV-2): two
// identifiers minted in the same minute differ however the random half falls.
func TestNewE2EIDIsUniquePerSequence(t *testing.T) {
	src := rng.New(rng.DefaultSeed)

	seen := make(map[string]bool)
	for seq := int64(1); seq <= 500; seq++ {
		id, err := core.NewE2EID(src, mintedAt, seq)
		if err != nil {
			t.Fatalf("NewE2EID(%d): %v", seq, err)
		}
		if seen[id] {
			t.Fatalf("duplicate identifier %q at sequence %d", id, seq)
		}
		seen[id] = true
	}
}

func TestNewE2EIDIsSeeded(t *testing.T) {
	first, err := core.NewE2EID(rng.New(rng.DefaultSeed), mintedAt, 7)
	if err != nil {
		t.Fatalf("NewE2EID: %v", err)
	}
	second, err := core.NewE2EID(rng.New(rng.DefaultSeed), mintedAt, 7)
	if err != nil {
		t.Fatalf("NewE2EID: %v", err)
	}
	if first != second {
		t.Errorf("equally seeded sources minted %q and %q", first, second)
	}
}

func TestNewIDRejectsSequenceOutOfRange(t *testing.T) {
	src := rng.New(rng.DefaultSeed)

	for _, seq := range []int64{0, -1, core.MaxIDSeq + 1} {
		if _, err := core.NewE2EID(src, mintedAt, seq); err == nil {
			t.Errorf("sequence %d was accepted", seq)
		}
	}
}

func TestNewRtrIDUsesRefundPrefix(t *testing.T) {
	id, err := core.NewRtrID(rng.New(rng.DefaultSeed), mintedAt, 1)
	if err != nil {
		t.Fatalf("NewRtrID: %v", err)
	}
	if !strings.HasPrefix(id, "D") {
		t.Errorf("rtrid = %q, want a D prefix", id)
	}
	if err := core.ValidateRtrID(id); err != nil {
		t.Errorf("minted rtrid does not validate: %v", err)
	}
	if err := core.ValidateE2EID(id); err == nil {
		t.Error("a rtrId validated as an e2eId")
	}
}

func TestValidateE2EID(t *testing.T) {
	const valid = "E12345678202607311204x7k2q90000f"

	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{name: "valid", in: valid, ok: true},
		{name: "empty", in: ""},
		{name: "too short", in: valid[:31]},
		{name: "too long", in: valid + "0"},
		{name: "wrong prefix", in: "X" + valid[1:]},
		{name: "letters in the ispb", in: "Eabcdefgh202607311204x7k2q90000f"},
		{name: "punctuation in the tail", in: valid[:31] + "-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := core.ValidateE2EID(tt.in)
			if tt.ok && err != nil {
				t.Errorf("ValidateE2EID(%q) = %v, want nil", tt.in, err)
			}
			if !tt.ok && err == nil {
				t.Errorf("ValidateE2EID(%q) = nil, want an error", tt.in)
			}
		})
	}
}

func TestValidateRefundID(t *testing.T) {
	tests := []struct {
		in string
		ok bool
	}{
		{in: "1", ok: true},
		{in: "devolucao1", ok: true},
		{in: strings.Repeat("a", 35), ok: true},
		{in: ""},
		{in: strings.Repeat("a", 36)},
		{in: "dev-1"},
		{in: "dev 1"},
	}

	for _, tt := range tests {
		err := core.ValidateRefundID(tt.in)
		if tt.ok && err != nil {
			t.Errorf("ValidateRefundID(%q) = %v, want nil", tt.in, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("ValidateRefundID(%q) = nil, want an error", tt.in)
		}
	}
}

func TestRefundableCents(t *testing.T) {
	p := core.Payment{AmountCents: 1000, RefundedCents: 250}
	if got := p.RefundableCents(); got != 750 {
		t.Errorf("RefundableCents() = %d, want 750", got)
	}
}

func TestValidateWebhookURL(t *testing.T) {
	tests := []struct {
		in string
		ok bool
	}{
		{in: "https://example.com/pix", ok: true},
		{in: "http://127.0.0.1:9999/hook", ok: true},
		{in: ""},
		{in: "ftp://example.com"},
		{in: "/relative/path"},
		{in: "https://" + strings.Repeat("a", 500)},
	}

	for _, tt := range tests {
		err := core.ValidateWebhookURL(tt.in)
		if tt.ok && err != nil {
			t.Errorf("ValidateWebhookURL(%q) = %v, want nil", tt.in, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("ValidateWebhookURL(%q) = nil, want an error", tt.in)
		}
	}
}
