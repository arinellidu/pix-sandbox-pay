// Package core holds the domain model: charges, money, identifiers. It is
// pure — no I/O, no HTTP, no SQL — so the rules can be tested on their own.
package core

import (
	"fmt"
	"strconv"
	"strings"
)

// Money lives as int64 cents everywhere inside the sandbox; the decimal string
// exists only at the API boundary, which is where BACEN specifies it:
// `valor.original` matches \d{1,10}\.\d{2}.

// maxAmountCents is ten integer digits' worth of currency, the widest value
// the API representation can carry.
const maxAmountCents = 9_999_999_999_99

// ParseAmount converts an API amount such as "10.00" into cents.
func ParseAmount(s string) (int64, error) {
	whole, frac, ok := strings.Cut(s, ".")
	if !ok {
		return 0, fmt.Errorf("amount %q must have two decimal places", s)
	}
	if len(whole) < 1 || len(whole) > 10 || len(frac) != 2 {
		return 0, fmt.Errorf("amount %q must match \\d{1,10}\\.\\d{2}", s)
	}
	if !isDigits(whole) || !isDigits(frac) {
		return 0, fmt.Errorf("amount %q must contain only digits and one dot", s)
	}

	units, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q is out of range", s)
	}
	cents, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q has invalid decimals", s)
	}

	total := units*100 + cents
	if total > maxAmountCents {
		return 0, fmt.Errorf("amount %q exceeds the maximum", s)
	}
	return total, nil
}

// FormatAmount renders cents as the API's decimal string.
func FormatAmount(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
