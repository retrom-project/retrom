package tagging

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// NormalizeName trims, NFC-normalizes and validates a tag display name.
// It returns the display form, case-folded key and canonical search text.
// This is a pure function with no I/O.
func NormalizeName(value string) (string, string, string, error) {
	normalized := norm.NFC.String(value)
	var builder strings.Builder
	spacePending := false
	started := false
	for _, character := range normalized {
		if unicode.IsControl(character) {
			return "", "", "", ErrNameInvalid
		}
		if unicode.IsSpace(character) {
			if started {
				spacePending = true
			}
			continue
		}
		if spacePending {
			builder.WriteByte(' ')
			spacePending = false
		}
		builder.WriteRune(character)
		started = true
	}
	display := builder.String()
	if display == "" || utf8.RuneCountInString(display) > MaximumNameRunes || len(display) > MaximumNameBytes {
		return "", "", "", ErrNameInvalid
	}
	return display, cases.Fold().String(display), CanonicalSearch(display), nil
}

// CanonicalSearch normalizes text for full-text search comparison.
func CanonicalSearch(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFC.String(value)), " "))
}

// ValidateIDs and ValidID are defined in validation.go.
