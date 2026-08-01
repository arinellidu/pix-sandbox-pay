package core

import (
	"fmt"
	"time"

	"github.com/arinelliquebec/pix-sandbox/internal/rng"
)

// Status is the lifecycle state of a charge.
//
// BACEN's own enum for `cob` is ATIVA, CONCLUIDA, REMOVIDA_PELO_USUARIO_RECEBEDOR
// and REMOVIDA_PELO_PSP — expiry is left for the client to derive from
// `calendario`. The sandbox materialises EXPIRADA instead, because the design
// models it as a state transition worth recording in the log.
type Status string

const (
	StatusAtiva                        Status = "ATIVA"
	StatusConcluida                    Status = "CONCLUIDA"
	StatusExpirada                     Status = "EXPIRADA"
	StatusRemovidaPeloUsuarioRecebedor Status = "REMOVIDA_PELO_USUARIO_RECEBEDOR"
	StatusRemovidaPeloPSP              Status = "REMOVIDA_PELO_PSP"
)

// DefaultExpiracao is BACEN's default for `calendario.expiracao`: one day.
const DefaultExpiracao int64 = 86400

// MaxExpiracao caps the window at roughly a year; past that a charge is not a
// charge.
const MaxExpiracao int64 = 365 * 24 * 60 * 60

// Devedor identifies the payer, when the payee chose to name one.
type Devedor struct {
	Nome string
	// CPF and CNPJ are digits only; at most one is set.
	CPF  string
	CNPJ string
}

// Charge is an immediate charge (`cob`).
type Charge struct {
	TxID               string
	Status             Status
	AmountCents        int64
	Chave              string
	SolicitacaoPagador string
	Devedor            *Devedor
	// Expiracao is the validity window in seconds from CreatedAt.
	Expiracao int64
	// EMV is the BR Code payload, rendered once at creation.
	EMV string
	// LocID identifies the payload location, mirroring BACEN's `loc.id`.
	LocID     int64
	Revisao   int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

// EffectiveStatus is the status a reader should see at instant now: an active
// charge past its window reads as EXPIRADA. Settled or removed charges keep
// the status they reached.
func (c Charge) EffectiveStatus(now time.Time) Status {
	if c.Status == StatusAtiva && !now.Before(c.ExpiresAt) {
		return StatusExpirada
	}
	return c.Status
}

// IsExpired reports whether an active charge has run out its window.
func (c Charge) IsExpired(now time.Time) bool {
	return c.Status == StatusAtiva && !now.Before(c.ExpiresAt)
}

// TxIDAlphabet is the character set BACEN allows in a txid.
const TxIDAlphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// txid length bounds from the API Pix specification.
const (
	MinTxIDLen = 26
	MaxTxIDLen = 35
	// GeneratedTxIDLen is what the sandbox mints when the caller supplies no
	// txid: comfortably inside the range and a familiar width.
	GeneratedTxIDLen = 32
)

// NewTxID mints a txid from the seeded source, so a given seed always yields
// the same sequence of identifiers.
func NewTxID(src *rng.Source) string {
	return src.String(GeneratedTxIDLen, TxIDAlphabet)
}

// ValidateTxID enforces ^[a-zA-Z0-9]{26,35}$.
func ValidateTxID(txid string) error {
	if len(txid) < MinTxIDLen || len(txid) > MaxTxIDLen {
		return fmt.Errorf("txid must be %d to %d characters, got %d", MinTxIDLen, MaxTxIDLen, len(txid))
	}
	for _, r := range txid {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		default:
			return fmt.Errorf("txid must be alphanumeric, found %q", r)
		}
	}
	return nil
}

// MaxChaveLen is the widest Pix key BACEN accepts.
const MaxChaveLen = 77

// ValidateChave checks the shape of a Pix key. Resolving it to an account is
// DICT's job and arrives with that emulator; here a key only has to be
// something a payload can carry.
func ValidateChave(chave string) error {
	if chave == "" {
		return fmt.Errorf("chave is required")
	}
	if len(chave) > MaxChaveLen {
		return fmt.Errorf("chave must be at most %d characters, got %d", MaxChaveLen, len(chave))
	}
	return nil
}

// MaxSolicitacaoPagadorLen is the specification's cap on the payer request.
const MaxSolicitacaoPagadorLen = 140

// ValidateDocument checks a CPF (11 digits) or CNPJ (14). Digits only — the
// sandbox never stores punctuation. Check digits are not verified: rejecting a
// well-formed test document would make fixtures harder to write than they need
// to be.
func ValidateDocument(doc string, want int, label string) error {
	if len(doc) != want {
		return fmt.Errorf("%s must have %d digits, got %d", label, want, len(doc))
	}
	if !isDigits(doc) {
		return fmt.Errorf("%s must contain digits only", label)
	}
	return nil
}
