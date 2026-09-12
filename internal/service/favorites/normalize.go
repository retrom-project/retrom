package favorites

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
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
			return "", "", ErrInvalidFolderName
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
		return "", "", ErrInvalidFolderName
	}
	return display, cases.Fold().String(display), nil
}

func ValidID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func validateUniqueIDs(values []string, maximum int) error {
	if len(values) > maximum {
		return ErrBatchTooLarge
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !ValidID(value) {
			return ErrInvalid
		}
		if _, exists := seen[value]; exists {
			return ErrInvalid
		}
		seen[value] = struct{}{}
	}
	return nil
}

func canonicalSearch(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
