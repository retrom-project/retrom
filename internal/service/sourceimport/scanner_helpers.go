package sourceimport

import (
	"errors"
	"sort"
	"strings"
	"unicode"

	pegasusmeta "retrom/internal/format/pegasus/meta"
)

func parserErrorCode(err error) string {
	switch {
	case errors.Is(err, pegasusmeta.ErrTooLarge):
		return pegasusmeta.ErrTooLarge.Error()
	case errors.Is(err, pegasusmeta.ErrInvalidUTF8):
		return pegasusmeta.ErrInvalidUTF8.Error()
	default:
		return pegasusmeta.ErrSyntax.Error()
	}
}

func parserDiagnostic(err error) (*int64, string) {
	var syntax *pegasusmeta.SyntaxError
	if errors.As(err, &syntax) {
		line := int64(syntax.Line)
		return &line, syntax.Reason
	}
	if errors.Is(err, pegasusmeta.ErrTooLarge) {
		return nil, "Metadata exceeds the 8 MiB limit."
	}
	return nil, "Metadata must contain valid UTF-8 text."
}

func asciiFold(value string) string {
	return strings.Map(func(character rune) rune {
		if character >= 'A' && character <= 'Z' {
			return character + ('a' - 'A')
		}
		return character
	}, value)
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stableStrings(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
