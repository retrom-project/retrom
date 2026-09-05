package packs

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func normalizeSourceNote(value string) (string, error) {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	value = strings.TrimSpace(norm.NFC.String(value))
	if utf8.RuneCountInString(value) > 500 || len(value) > 2000 {
		return "", ErrInvalid
	}
	for _, current := range value {
		if current <= 0x08 || current == 0x0b || current == 0x0c ||
			current >= 0x0e && current <= 0x1f || current >= 0x7f && current <= 0x9f {
			return "", ErrInvalid
		}
	}
	return value, nil
}
