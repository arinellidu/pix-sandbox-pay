package core

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/arinellidu/pix-sandbox-pay/internal/rng"
)

// SandboxISPB signs every identifier the sandbox mints. 12345678 belongs to no
// real participant, so a sandbox e2eId can never be mistaken for one that
// crossed the real SPI.
const SandboxISPB = "12345678"

// Identifiers are 32 characters, in the shape BACEN specifies:
//
//	E 12345678 202607311204 x7k2q9 0000f
//	│ │        │            │      └ base-36 sequence: unique by construction
//	│ │        │            └ six characters from the seeded source
//	│ │        └ minute-precision UTC timestamp
//	│ └ ISPB of the issuing participant
//	└ E for a payment, D for a refund
//
// The trailing sequence is what guarantees uniqueness (INV-2): two identifiers
// minted in the same minute cannot collide however the random half falls. The
// random characters are there so the identifiers look like the real thing and
// stay reproducible under a fixed seed.
const (
	e2eIDPrefix  = "E"
	rtrIDPrefix  = "D"
	idLen        = 32
	idDigitsLen  = 20 // ISPB (8) + timestamp (12)
	idTimeLayout = "200601021504"
	idRandomLen  = 6
	idSeqLen     = 5
)

// MaxIDSeq is the largest sequence a five-character base-36 field can carry.
const MaxIDSeq int64 = 60466175 // 36^5 - 1

// NewE2EID mints the identifier of a settled payment.
func NewE2EID(src *rng.Source, now time.Time, seq int64) (string, error) {
	return newTransactionID(e2eIDPrefix, src, now, seq)
}

// NewRtrID mints the identifier of a refund.
func NewRtrID(src *rng.Source, now time.Time, seq int64) (string, error) {
	return newTransactionID(rtrIDPrefix, src, now, seq)
}

func newTransactionID(prefix string, src *rng.Source, now time.Time, seq int64) (string, error) {
	if seq < 1 || seq > MaxIDSeq {
		return "", fmt.Errorf("identifier sequence %d is outside 1..%d", seq, MaxIDSeq)
	}
	encoded := strconv.FormatInt(seq, 36)
	return prefix +
		SandboxISPB +
		now.UTC().Format(idTimeLayout) +
		src.String(idRandomLen, TxIDAlphabet) +
		strings.Repeat("0", idSeqLen-len(encoded)) + encoded, nil
}

// ValidateE2EID enforces ^E[0-9]{20}[a-zA-Z0-9]{11}$.
func ValidateE2EID(id string) error { return validateTransactionID(id, e2eIDPrefix, "endToEndId") }

// ValidateRtrID enforces the same shape with the refund prefix.
func ValidateRtrID(id string) error { return validateTransactionID(id, rtrIDPrefix, "rtrId") }

func validateTransactionID(id, prefix, label string) error {
	if len(id) != idLen {
		return fmt.Errorf("%s must have %d characters, got %d", label, idLen, len(id))
	}
	if !strings.HasPrefix(id, prefix) {
		return fmt.Errorf("%s must start with %q", label, prefix)
	}
	if !isDigits(id[1 : 1+idDigitsLen]) {
		return fmt.Errorf("%s must carry an ISPB and a timestamp in digits", label)
	}
	if !isAlnum(id[1+idDigitsLen:]) {
		return fmt.Errorf("%s must end in %d alphanumeric characters", label, idLen-idDigitsLen-1)
	}
	return nil
}

// PaymentStatus is where a payment sits in the machine of DESIGN §4. It is
// internal: the API Pix has no status field on a `Pix`, because a payment only
// becomes visible to the payee once it has settled.
type PaymentStatus string

const (
	PaymentSettled  PaymentStatus = "SETTLED"
	PaymentRefunded PaymentStatus = "REFUNDED"
)

// MaxInfoPagadorLen is the specification's cap on the payer's message.
const MaxInfoPagadorLen = 140

// Payment is a pix the payee received.
type Payment struct {
	E2EID string
	// Seq is the monotonic number the identifier encodes.
	Seq           int64
	TxID          string
	Chave         string
	Status        PaymentStatus
	AmountCents   int64
	RefundedCents int64
	InfoPagador   string
	// CreatedAt is `horario` in the API: when the payment settled.
	CreatedAt time.Time
}

// RefundableCents is what is left of a payment to give back (INV-4).
func (p Payment) RefundableCents() int64 { return p.AmountCents - p.RefundedCents }

// RefundStatus is the API-visible state of a `devolucao`.
type RefundStatus string

const (
	RefundEmProcessamento RefundStatus = "EM_PROCESSAMENTO"
	RefundDevolvido       RefundStatus = "DEVOLVIDO"
	RefundNaoRealizado    RefundStatus = "NAO_REALIZADO"
)

// MaxRefundIDLen is the specification's cap on the payee-chosen refund id.
const MaxRefundIDLen = 35

// Refund is a devolução raised against a payment.
type Refund struct {
	// ID is chosen by the payee and unique within the payment.
	ID          string
	E2EID       string
	RtrID       string
	Seq         int64
	AmountCents int64
	Status      RefundStatus
	Motivo      string
	// RequestedAt and SettledAt are `horario.solicitacao` and
	// `horario.liquidacao`. Without the chaos API or the virtual clock the two
	// are the same instant; both are recorded so the log keeps the shape it
	// will need once they can differ.
	RequestedAt time.Time
	SettledAt   time.Time
}

// ValidateRefundID enforces ^[a-zA-Z0-9]{1,35}$.
func ValidateRefundID(id string) error {
	if id == "" || len(id) > MaxRefundIDLen {
		return fmt.Errorf("id must be 1 to %d characters, got %d", MaxRefundIDLen, len(id))
	}
	if !isAlnum(id) {
		return fmt.Errorf("id must be alphanumeric")
	}
	return nil
}

func isAlnum(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		default:
			return false
		}
	}
	return true
}
