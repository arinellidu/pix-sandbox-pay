package emv

import (
	"fmt"
	"strconv"
	"strings"
)

// TLV encoding, the EMV QR Code payload format: a two-digit id, a two-digit
// length, then that many characters of value. Templates (such as field 26)
// carry the same structure recursively in their value.

// maxValueLen is what two decimal length digits can express.
const maxValueLen = 99

// TLV is one id/value pair.
type TLV struct {
	ID    string
	Value string
}

// Encode renders one field. It fails on ids that are not two digits and on
// values too long to describe with a two-digit length.
func Encode(id, value string) (string, error) {
	if len(id) != 2 || !isDigits(id) {
		return "", fmt.Errorf("emv: field id %q must be two digits", id)
	}
	if len(value) > maxValueLen {
		return "", fmt.Errorf("emv: field %s value is %d chars, max %d", id, len(value), maxValueLen)
	}
	return fmt.Sprintf("%s%02d%s", id, len(value), value), nil
}

// EncodeAll renders fields in the order given; EMV requires ascending ids, and
// callers are expected to supply them that way.
func EncodeAll(fields []TLV) (string, error) {
	var sb strings.Builder
	for _, f := range fields {
		s, err := Encode(f.ID, f.Value)
		if err != nil {
			return "", err
		}
		sb.WriteString(s)
	}
	return sb.String(), nil
}

// Parse splits one level of TLV. It is the inverse of EncodeAll and exists so
// tests can assert on structure rather than on substrings.
func Parse(payload string) ([]TLV, error) {
	var out []TLV
	for i := 0; i < len(payload); {
		if i+4 > len(payload) {
			return nil, fmt.Errorf("emv: truncated field header at offset %d", i)
		}
		id := payload[i : i+2]
		if !isDigits(id) {
			return nil, fmt.Errorf("emv: field id %q at offset %d is not numeric", id, i)
		}
		length, err := strconv.Atoi(payload[i+2 : i+4])
		if err != nil {
			return nil, fmt.Errorf("emv: bad length for field %s at offset %d: %w", id, i, err)
		}
		if i+4+length > len(payload) {
			return nil, fmt.Errorf("emv: field %s claims %d chars but payload ends early", id, length)
		}
		out = append(out, TLV{ID: id, Value: payload[i+4 : i+4+length]})
		i += 4 + length
	}
	return out, nil
}

// Find returns the value of id among fields.
func Find(fields []TLV, id string) (string, bool) {
	for _, f := range fields {
		if f.ID == id {
			return f.Value, true
		}
	}
	return "", false
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}
