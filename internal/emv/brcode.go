// Package emv builds Pix BR Code payloads: EMV QR Code TLV encoding plus the
// br.gov.bcb.pix merchant account template and the CRC16 that closes it.
//
// The sandbox emits the self-contained flavour: the Pix key travels in 26-01
// and the txid in 62-05, so a payload resolves without fetching a location
// document. The strict dynamic flavour (URL in 26-25, "***" in 62-05) needs an
// endpoint serving the charge, which lands with the console work.
package emv

import (
	"fmt"
	"strings"
)

// Pix reserves this globally unique identifier for its merchant account
// template; it is what marks a BR Code as a Pix payload.
const GUI = "br.gov.bcb.pix"

// Field ids of the EMV QR Code specification used by a Pix payload.
const (
	FieldPayloadFormat     = "00"
	FieldPointOfInitiation = "01"
	FieldMerchantAccount   = "26"
	FieldMerchantCategory  = "52"
	FieldCurrency          = "53"
	FieldAmount            = "54"
	FieldCountry           = "58"
	FieldMerchantName      = "59"
	FieldMerchantCity      = "60"
	FieldAdditionalData    = "62"
	FieldCRC               = "63"

	// Subfields of the merchant account template (26).
	SubGUI         = "00"
	SubKey         = "01"
	SubDescription = "02"

	// Subfield of the additional data template (62).
	SubTxID = "05"
)

const (
	payloadFormatVersion = "01"
	initiationOneShot    = "12" // single payment
	initiationReusable   = "11" // reusable payload
	merchantCategoryNone = "0000"
	currencyBRL          = "986" // ISO 4217
	countryBR            = "BR"

	// Lengths the specification caps these fields at.
	maxMerchantName = 25
	maxMerchantCity = 15
	maxDescription  = 72
	maxAmount       = 13
)

// Defaults used when the caller leaves the merchant identification empty.
const (
	DefaultMerchantName = "PIX SANDBOX"
	DefaultMerchantCity = "SAO PAULO"
)

// BRCode is the input to a Pix payload.
type BRCode struct {
	// Key is the Pix key of the payee (26-01). Required.
	Key string
	// TxID goes in 62-05. Empty is encoded as "***", which the specification
	// reads as "no identifier".
	TxID string
	// Amount is the transaction value formatted with two decimals, e.g.
	// "10.00" (54). Empty omits the field, letting the payer choose.
	Amount string
	// Description is free text about the charge (26-02). Optional.
	Description string
	// MerchantName (59) and MerchantCity (60) identify the payee. They are
	// uppercased and stripped of accents; defaults apply when empty.
	MerchantName string
	MerchantCity string
	// Reusable marks the payload as one that may be paid more than once (01).
	Reusable bool
}

// NoTxID is what 62-05 carries when a payload identifies no transaction.
const NoTxID = "***"

// Payload renders the BR Code, CRC included.
func (b BRCode) Payload() (string, error) {
	if b.Key == "" {
		return "", fmt.Errorf("emv: pix key is required")
	}

	account, err := b.merchantAccount()
	if err != nil {
		return "", err
	}
	additional, err := b.additionalData()
	if err != nil {
		return "", err
	}

	initiation := initiationOneShot
	if b.Reusable {
		initiation = initiationReusable
	}

	fields := []TLV{
		{ID: FieldPayloadFormat, Value: payloadFormatVersion},
		{ID: FieldPointOfInitiation, Value: initiation},
		{ID: FieldMerchantAccount, Value: account},
		{ID: FieldMerchantCategory, Value: merchantCategoryNone},
		{ID: FieldCurrency, Value: currencyBRL},
	}
	if b.Amount != "" {
		if len(b.Amount) > maxAmount {
			return "", fmt.Errorf("emv: amount %q exceeds %d chars", b.Amount, maxAmount)
		}
		fields = append(fields, TLV{ID: FieldAmount, Value: b.Amount})
	}
	fields = append(fields,
		TLV{ID: FieldCountry, Value: countryBR},
		TLV{ID: FieldMerchantName, Value: sanitize(b.MerchantName, DefaultMerchantName, maxMerchantName)},
		TLV{ID: FieldMerchantCity, Value: sanitize(b.MerchantCity, DefaultMerchantCity, maxMerchantCity)},
		TLV{ID: FieldAdditionalData, Value: additional},
	)

	body, err := EncodeAll(fields)
	if err != nil {
		return "", err
	}

	// The CRC covers the payload including its own id and length, so the
	// "6304" header is appended before computing it.
	body += FieldCRC + "04"
	return body + checksum(body), nil
}

func (b BRCode) merchantAccount() (string, error) {
	subfields := []TLV{
		{ID: SubGUI, Value: GUI},
		{ID: SubKey, Value: b.Key},
	}
	if b.Description != "" {
		description := b.Description
		if len(description) > maxDescription {
			description = description[:maxDescription]
		}
		subfields = append(subfields, TLV{ID: SubDescription, Value: description})
	}
	return EncodeAll(subfields)
}

func (b BRCode) additionalData() (string, error) {
	txid := b.TxID
	if txid == "" {
		txid = NoTxID
	}
	return EncodeAll([]TLV{{ID: SubTxID, Value: txid}})
}

// Verify recomputes the CRC of a payload and compares it with the one carried
// in field 63.
func Verify(payload string) error {
	const crcFieldLen = 8 // "63" + "04" + four hex digits
	if len(payload) < crcFieldLen {
		return fmt.Errorf("emv: payload too short to hold a CRC")
	}
	head, tail := payload[:len(payload)-4], payload[len(payload)-4:]
	if !strings.HasSuffix(head, FieldCRC+"04") {
		return fmt.Errorf("emv: payload does not end with field 63")
	}
	if want := checksum(head); !strings.EqualFold(tail, want) {
		return fmt.Errorf("emv: CRC mismatch: payload carries %s, computed %s", tail, want)
	}
	return nil
}

// accents maps the Latin-1 letters Portuguese needs onto their ASCII base.
// EMV fields 59 and 60 are limited to the invariant character set, and a
// stripped name beats a payload some readers reject.
var accents = strings.NewReplacer(
	"Á", "A", "À", "A", "Â", "A", "Ã", "A", "Ä", "A",
	"É", "E", "È", "E", "Ê", "E", "Ë", "E",
	"Í", "I", "Ì", "I", "Î", "I", "Ï", "I",
	"Ó", "O", "Ò", "O", "Ô", "O", "Õ", "O", "Ö", "O",
	"Ú", "U", "Ù", "U", "Û", "U", "Ü", "U",
	"Ç", "C", "Ñ", "N",
)

// sanitize uppercases, strips accents and anything outside the invariant set,
// collapses whitespace, and truncates to max. Empty input yields fallback.
func sanitize(s, fallback string, max int) string {
	s = accents.Replace(strings.ToUpper(strings.TrimSpace(s)))

	var sb strings.Builder
	lastSpace := false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			sb.WriteRune(r)
			lastSpace = false
		case r == ' ' && !lastSpace && sb.Len() > 0:
			sb.WriteRune(r)
			lastSpace = true
		}
	}

	out := strings.TrimSpace(sb.String())
	if out == "" {
		out = fallback
	}
	if len(out) > max {
		out = strings.TrimSpace(out[:max])
	}
	return out
}
