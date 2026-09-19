package favorites

import (
	"strings"
	"unicode"
	"unicode/utf8"

	model "retrom/internal/model/favorites"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

func NormalizeFolderName(value string) (string, string, error) {
	normalized := norm.NFC.String(value)
	var builder strings.Builder
	spacePending := false
	started := false
	for _, character := range normalized {
		if unicode.IsControl(character) {
			return "", "", model.ErrInvalidFolderName
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
	if display == "" || utf8.RuneCountInString(display) > 40 || len([]byte(display)) > 160 {
		return "", "", model.ErrInvalidFolderName
	}
	return display, cases.Fold().String(display), nil
}

func validateUniqueIDs(values []string, maximum int) error {
	if len(values) > maximum {
		return model.ErrBatchTooLarge
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !model.ValidID(value) {
			return model.ErrInvalid
		}
		if _, exists := seen[value]; exists {
			return model.ErrInvalid
		}
		seen[value] = struct{}{}
	}
	return nil
}

func canonicalSearch(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
