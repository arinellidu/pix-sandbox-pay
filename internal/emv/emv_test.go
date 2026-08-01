package emv_test

import (
	"strings"
	"testing"

	"github.com/arinellidu/pix-sandbox-pay/internal/emv"
)

// bacenExample is the static BR Code printed in BACEN's manual, CRC included.
// It is an outside fixture: nothing in this repository produced it, so it
// pins the encoding as much as the checksum.
const bacenExample = "00020126580014br.gov.bcb.pix0136123e4567-e12b-12d1-a456-4266554400005204" +
	"000053039865802BR5913Fulano de Tal6008BRASILIA62070503***63041D3D"

func TestCRC16CheckValue(t *testing.T) {
	// The universally published check value for CRC-16/CCITT-FALSE: the
	// string "123456789" hashes to 0x29B1. If this fails, the polynomial,
	// the initial value or the reflection settings are wrong.
	if got := emv.CRC16([]byte("123456789")); got != 0x29B1 {
		t.Errorf("CRC16(\"123456789\") = %04X, want 29B1", got)
	}
}

func TestCRC16Vectors(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			// Example payload from BACEN's BR Code manual, up to and
			// including the "6304" header: a static code with no amount,
			// lowercase merchant name and all.
			name:    "bacen manual example",
			payload: bacenExample[:len(bacenExample)-4],
			want:    "1D3D",
		},
		{
			name:    "empty",
			payload: "",
			want:    "FFFF",
		},
		{
			name:    "single A",
			payload: "A",
			want:    "B915",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := emv.CRC16([]byte(tt.payload))
			if hex := strings.ToUpper(hexString(got)); hex != tt.want {
				t.Errorf("CRC16(%q) = %s, want %s", tt.payload, hex, tt.want)
			}
		})
	}
}

func hexString(v uint16) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{
		digits[v>>12&0xF], digits[v>>8&0xF], digits[v>>4&0xF], digits[v&0xF],
	})
}

func TestVerifyAcceptsBacenExample(t *testing.T) {
	if err := emv.Verify(bacenExample); err != nil {
		t.Errorf("Verify(bacen example) = %v, want nil", err)
	}
}

// The manual's example must also survive a round trip through the parser: its
// structure, not just its checksum, is the thing being pinned.
func TestParseBacenExample(t *testing.T) {
	fields, err := emv.Parse(bacenExample)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	account, ok := emv.Find(fields, emv.FieldMerchantAccount)
	if !ok {
		t.Fatal("merchant account template missing")
	}
	sub, err := emv.Parse(account)
	if err != nil {
		t.Fatalf("parse merchant account: %v", err)
	}
	if gui, _ := emv.Find(sub, emv.SubGUI); gui != emv.GUI {
		t.Errorf("GUI = %q, want %q", gui, emv.GUI)
	}
	if key, _ := emv.Find(sub, emv.SubKey); key != "123e4567-e12b-12d1-a456-426655440000" {
		t.Errorf("key = %q, want the example EVP", key)
	}
	if _, ok := emv.Find(fields, emv.FieldAmount); ok {
		t.Error("field 54 present; the manual's example carries no amount")
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	code := emv.BRCode{Key: "dev@example.com", TxID: strings.Repeat("a", 26), Amount: "10.00"}
	payload, err := code.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}

	// Flip one character of the amount; the CRC must stop agreeing.
	tampered := strings.Replace(payload, "540510.00", "540520.00", 1)
	if tampered == payload {
		t.Fatal("test setup failed to alter the payload")
	}
	if err := emv.Verify(tampered); err == nil {
		t.Error("Verify(tampered) = nil, want a CRC mismatch")
	}
}

func TestEncode(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		value   string
		want    string
		wantErr bool
	}{
		{name: "short value", id: "00", value: "01", want: "000201"},
		{name: "length is zero padded", id: "53", value: "986", want: "5303986"},
		{name: "empty value", id: "62", value: "", want: "6200"},
		{name: "two digit length", id: "26", value: strings.Repeat("x", 58), want: "2658" + strings.Repeat("x", 58)},
		{name: "value too long", id: "26", value: strings.Repeat("x", 100), wantErr: true},
		{name: "id not numeric", id: "ab", value: "x", wantErr: true},
		{name: "id too short", id: "0", value: "x", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := emv.Encode(tt.id, tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Encode(%q, ...) = %q, want error", tt.id, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if got != tt.want {
				t.Errorf("Encode(%q, %q) = %q, want %q", tt.id, tt.value, got, tt.want)
			}
		})
	}
}

func TestParseRoundTrip(t *testing.T) {
	fields := []emv.TLV{
		{ID: "00", Value: "01"},
		{ID: "26", Value: "0014" + emv.GUI},
		{ID: "53", Value: "986"},
	}

	encoded, err := emv.EncodeAll(fields)
	if err != nil {
		t.Fatalf("EncodeAll: %v", err)
	}
	parsed, err := emv.Parse(encoded)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(parsed) != len(fields) {
		t.Fatalf("parsed %d fields, want %d", len(parsed), len(fields))
	}
	for i := range fields {
		if parsed[i] != fields[i] {
			t.Errorf("field %d = %+v, want %+v", i, parsed[i], fields[i])
		}
	}
}

func TestParseRejectsMalformed(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{name: "truncated header", payload: "000"},
		{name: "length past end", payload: "0099x"},
		{name: "non numeric id", payload: "ab02xy"},
		// Atoi accepts signed input; a negative length inverted the value
		// slice and panicked instead of erroring.
		{name: "negative length", payload: "00-1"},
		{name: "negative zero length", payload: "00-0"},
		{name: "plus signed length", payload: "00+2ab"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := emv.Parse(tt.payload); err == nil {
				t.Errorf("Parse(%q) = nil error, want failure", tt.payload)
			}
		})
	}
}

func TestPayloadStructure(t *testing.T) {
	const txid = "abc123def456ghi789jkl012mn"

	code := emv.BRCode{
		Key:          "dev@example.com",
		TxID:         txid,
		Amount:       "10.00",
		MerchantName: "Padaria São João",
		MerchantCity: "São Paulo",
	}
	payload, err := code.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}
	if err := emv.Verify(payload); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	fields, err := emv.Parse(payload)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := map[string]string{
		emv.FieldPayloadFormat:     "01",
		emv.FieldPointOfInitiation: "12",
		emv.FieldMerchantCategory:  "0000",
		emv.FieldCurrency:          "986",
		emv.FieldAmount:            "10.00",
		emv.FieldCountry:           "BR",
		// Accents stripped, uppercased.
		emv.FieldMerchantName: "PADARIA SAO JOAO",
		emv.FieldMerchantCity: "SAO PAULO",
	}
	for id, value := range want {
		got, ok := emv.Find(fields, id)
		if !ok {
			t.Errorf("field %s missing", id)
			continue
		}
		if got != value {
			t.Errorf("field %s = %q, want %q", id, got, value)
		}
	}

	// Ids must climb: EMV requires ascending order.
	for i := 1; i < len(fields); i++ {
		if fields[i].ID <= fields[i-1].ID {
			t.Errorf("fields out of order at %d: %s after %s", i, fields[i].ID, fields[i-1].ID)
		}
	}

	account, ok := emv.Find(fields, emv.FieldMerchantAccount)
	if !ok {
		t.Fatal("merchant account template missing")
	}
	sub, err := emv.Parse(account)
	if err != nil {
		t.Fatalf("parse merchant account: %v", err)
	}
	if gui, _ := emv.Find(sub, emv.SubGUI); gui != emv.GUI {
		t.Errorf("GUI = %q, want %q", gui, emv.GUI)
	}
	if key, _ := emv.Find(sub, emv.SubKey); key != "dev@example.com" {
		t.Errorf("key = %q, want dev@example.com", key)
	}

	additional, ok := emv.Find(fields, emv.FieldAdditionalData)
	if !ok {
		t.Fatal("additional data template missing")
	}
	sub, err = emv.Parse(additional)
	if err != nil {
		t.Fatalf("parse additional data: %v", err)
	}
	// A cob txid (26-35 chars) never fits 62-05's 25-char cap: the payload
	// must degrade to "***" rather than emit a field readers reject.
	if got, _ := emv.Find(sub, emv.SubTxID); got != emv.NoTxID {
		t.Errorf("txid = %q, want %q for a %d-char txid", got, emv.NoTxID, len(txid))
	}
}

func TestPayloadShortTxIDTravelsIn6205(t *testing.T) {
	const txid = "demo123" // static-QR ids up to 25 chars fit the field
	code := emv.BRCode{Key: "dev@example.com", TxID: txid}
	payload, err := code.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}

	fields, _ := emv.Parse(payload)
	additional, _ := emv.Find(fields, emv.FieldAdditionalData)
	sub, _ := emv.Parse(additional)
	if got, _ := emv.Find(sub, emv.SubTxID); got != txid {
		t.Errorf("txid = %q, want %q", got, txid)
	}
}

func TestPayloadOmitsEmptyAmount(t *testing.T) {
	code := emv.BRCode{Key: "dev@example.com", TxID: "***"}
	payload, err := code.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}

	fields, err := emv.Parse(payload)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := emv.Find(fields, emv.FieldAmount); ok {
		t.Error("field 54 present, want omitted when the amount is empty")
	}
}

func TestPayloadEmptyTxIDBecomesStars(t *testing.T) {
	code := emv.BRCode{Key: "dev@example.com"}
	payload, err := code.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}

	fields, _ := emv.Parse(payload)
	additional, _ := emv.Find(fields, emv.FieldAdditionalData)
	sub, _ := emv.Parse(additional)
	if got, _ := emv.Find(sub, emv.SubTxID); got != emv.NoTxID {
		t.Errorf("txid = %q, want %q", got, emv.NoTxID)
	}
}

func TestPayloadReusableFlag(t *testing.T) {
	code := emv.BRCode{Key: "dev@example.com", Reusable: true}
	payload, err := code.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}

	fields, _ := emv.Parse(payload)
	if got, _ := emv.Find(fields, emv.FieldPointOfInitiation); got != "11" {
		t.Errorf("field 01 = %q, want 11 for a reusable payload", got)
	}
}

func TestPayloadRequiresKey(t *testing.T) {
	if _, err := (emv.BRCode{TxID: "x"}).Payload(); err == nil {
		t.Error("Payload without a key = nil error, want failure")
	}
}

func TestPayloadDeterministic(t *testing.T) {
	code := emv.BRCode{Key: "dev@example.com", TxID: "abc123def456ghi789jkl012mn", Amount: "10.00"}

	first, err := code.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}
	second, _ := code.Payload()
	if first != second {
		t.Errorf("payload not stable:\n%s\n%s", first, second)
	}
}

func TestSanitizeFallbacks(t *testing.T) {
	code := emv.BRCode{Key: "dev@example.com", MerchantName: "!!!", MerchantCity: ""}
	payload, err := code.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}

	fields, _ := emv.Parse(payload)
	if got, _ := emv.Find(fields, emv.FieldMerchantName); got != emv.DefaultMerchantName {
		t.Errorf("merchant name = %q, want the default %q", got, emv.DefaultMerchantName)
	}
	if got, _ := emv.Find(fields, emv.FieldMerchantCity); got != emv.DefaultMerchantCity {
		t.Errorf("merchant city = %q, want the default %q", got, emv.DefaultMerchantCity)
	}
}

func TestSanitizeTruncates(t *testing.T) {
	code := emv.BRCode{
		Key:          "dev@example.com",
		MerchantName: strings.Repeat("A", 40),
		MerchantCity: strings.Repeat("B", 40),
	}
	payload, err := code.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}

	fields, _ := emv.Parse(payload)
	name, _ := emv.Find(fields, emv.FieldMerchantName)
	if len(name) != 25 {
		t.Errorf("merchant name length = %d, want 25", len(name))
	}
	city, _ := emv.Find(fields, emv.FieldMerchantCity)
	if len(city) != 15 {
		t.Errorf("merchant city length = %d, want 15", len(city))
	}
}
