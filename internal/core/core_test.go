package core_test

import (
	"strings"
	"testing"
	"time"

	"github.com/arinellidu/pix-sandbox-pay/internal/core"
	"github.com/arinellidu/pix-sandbox-pay/internal/rng"
)

func TestParseAmount(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{in: "10.00", want: 1000},
		{in: "0.01", want: 1},
		{in: "0.00", want: 0},
		{in: "1234567890.99", want: 123456789099},
		{in: "9999999999.99", want: 999999999999},
		{in: "10", wantErr: true},
		{in: "10.0", wantErr: true},
		{in: "10.000", wantErr: true},
		{in: "10,00", wantErr: true},
		{in: "-1.00", wantErr: true},
		{in: "", wantErr: true},
		{in: ".00", wantErr: true},
		{in: "1.0a", wantErr: true},
		{in: "12345678901.00", wantErr: true},
		{in: "1.00.00", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := core.ParseAmount(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseAmount(%q) = %d, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAmount(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseAmount(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatAmount(t *testing.T) {
	tests := []struct {
		cents int64
		want  string
	}{
		{cents: 1000, want: "10.00"},
		{cents: 1, want: "0.01"},
		{cents: 0, want: "0.00"},
		{cents: 99, want: "0.99"},
		{cents: 100000, want: "1000.00"},
		{cents: 123456789099, want: "1234567890.99"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := core.FormatAmount(tt.cents); got != tt.want {
				t.Errorf("FormatAmount(%d) = %q, want %q", tt.cents, got, tt.want)
			}
		})
	}
}

func TestAmountRoundTrip(t *testing.T) {
	for _, s := range []string{"0.00", "0.07", "10.00", "99.99", "1234567890.99"} {
		cents, err := core.ParseAmount(s)
		if err != nil {
			t.Fatalf("ParseAmount(%q): %v", s, err)
		}
		if got := core.FormatAmount(cents); got != s {
			t.Errorf("round trip of %q gave %q", s, got)
		}
	}
}

func TestValidateTxID(t *testing.T) {
	tests := []struct {
		name    string
		txid    string
		wantErr bool
	}{
		{name: "minimum length", txid: strings.Repeat("a", 26)},
		{name: "maximum length", txid: strings.Repeat("a", 35)},
		{name: "mixed case and digits", txid: "abc123DEF456ghi789JKL012mno345"},
		{name: "too short", txid: strings.Repeat("a", 25), wantErr: true},
		{name: "too long", txid: strings.Repeat("a", 36), wantErr: true},
		{name: "empty", txid: "", wantErr: true},
		{name: "hyphen", txid: "abc-123def456ghi789jkl012mn", wantErr: true},
		{name: "space", txid: "abc 123def456ghi789jkl012mn", wantErr: true},
		{name: "accented", txid: "abcç123def456ghi789jkl012mn", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := core.ValidateTxID(tt.txid)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateTxID(%q) = nil, want error", tt.txid)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateTxID(%q) = %v, want nil", tt.txid, err)
			}
		})
	}
}

func TestNewTxIDIsValidAndSeeded(t *testing.T) {
	first := core.NewTxID(rng.New(99))
	if err := core.ValidateTxID(first); err != nil {
		t.Fatalf("generated txid rejected: %v", err)
	}
	if len(first) != core.GeneratedTxIDLen {
		t.Errorf("length = %d, want %d", len(first), core.GeneratedTxIDLen)
	}

	// Same seed, same identifier: reproducible runs (ADR-007).
	if second := core.NewTxID(rng.New(99)); second != first {
		t.Errorf("seeded txid not reproducible: %q then %q", first, second)
	}

	// Successive draws from one source must differ (INV-2 depends on it).
	src := rng.New(99)
	seen := make(map[string]bool)
	for range 1000 {
		id := core.NewTxID(src)
		if seen[id] {
			t.Fatalf("txid %q generated twice", id)
		}
		seen[id] = true
	}
}

func TestEffectiveStatus(t *testing.T) {
	created := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	expires := created.Add(time.Hour)

	tests := []struct {
		name   string
		status core.Status
		now    time.Time
		want   core.Status
	}{
		{name: "active inside window", status: core.StatusAtiva, now: created.Add(time.Minute), want: core.StatusAtiva},
		{name: "active at the boundary", status: core.StatusAtiva, now: expires, want: core.StatusExpirada},
		{name: "active past the window", status: core.StatusAtiva, now: expires.Add(time.Second), want: core.StatusExpirada},
		{name: "settled charges do not expire", status: core.StatusConcluida, now: expires.Add(time.Hour), want: core.StatusConcluida},
		{name: "removed charges do not expire", status: core.StatusRemovidaPeloUsuarioRecebedor, now: expires.Add(time.Hour), want: core.StatusRemovidaPeloUsuarioRecebedor},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			charge := core.Charge{Status: tt.status, CreatedAt: created, ExpiresAt: expires}
			if got := charge.EffectiveStatus(tt.now); got != tt.want {
				t.Errorf("EffectiveStatus = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateChave(t *testing.T) {
	tests := []struct {
		name    string
		chave   string
		wantErr bool
	}{
		{name: "email", chave: "dev@example.com"},
		{name: "phone", chave: "+5511999999999"},
		{name: "cpf", chave: "12345678909"},
		{name: "evp", chave: "123e4567-e12b-12d1-a456-426655440000"},
		{name: "empty", chave: "", wantErr: true},
		{name: "too long", chave: strings.Repeat("a", 78), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := core.ValidateChave(tt.chave)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateChave(%q) = nil, want error", tt.chave)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateChave(%q) = %v, want nil", tt.chave, err)
			}
		})
	}
}

func TestValidateDocument(t *testing.T) {
	if err := core.ValidateDocument("12345678909", 11, "cpf"); err != nil {
		t.Errorf("valid cpf rejected: %v", err)
	}
	if err := core.ValidateDocument("12345678000199", 14, "cnpj"); err != nil {
		t.Errorf("valid cnpj rejected: %v", err)
	}
	if err := core.ValidateDocument("123.456.789-09", 11, "cpf"); err == nil {
		t.Error("punctuated cpf accepted, want digits only")
	}
	if err := core.ValidateDocument("1234567890", 11, "cpf"); err == nil {
		t.Error("short cpf accepted")
	}
}
