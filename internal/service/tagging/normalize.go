package tagging

import (
	"strings"
	"unicode"
	"unicode/utf8"

	model "retrom/internal/model/tagging"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

func NormalizeName(value string) (string, string, string, error) {
	normalized := norm.NFC.String(value)
	var builder strings.Builder
	spacePending := false
	started := false
	for _, character := range normalized {
		if unicode.IsControl(character) {
			return "", "", "", model.ErrNameInvalid
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
	if display == "" || utf8.RuneCountInString(display) > model.MaximumNameRunes || len(display) > model.MaximumNameBytes {
		return "", "", "", model.ErrNameInvalid
	}
	return display, cases.Fold().String(display), canonicalSearch(display), nil
}

func canonicalSearch(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFC.String(value)), " "))
}
