package tagging

import (
	"strings"
	"unicode"
	"unicode/utf8"

	model "retrom/internal/model/tagging"

	"github.com/google/uuid"
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

func ValidID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value && parsed.Version() == 7
}

func ValidateIDs(values []string) ([]string, error) {
	if values == nil || len(values) > model.MaxTagsPerOwner {
		if len(values) > model.MaxTagsPerOwner {
			return nil, model.ErrAssignmentLimitExceeded
		}
		return nil, model.ErrInvalid
	}
	result := append([]string{}, values...)
	seen := make(map[string]struct{}, len(result))
	for _, value := range result {
		if !ValidID(value) {
			return nil, model.ErrInvalid
		}
		if _, exists := seen[value]; exists {
			return nil, model.ErrInvalid
		}
		seen[value] = struct{}{}
	}
	return result, nil
}
