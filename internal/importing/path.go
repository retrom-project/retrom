package importing

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrUnsafeLogicalPath = errors.New("UNSAFE_LOGICAL_PATH")

//nolint:gocyclo // Independent path safety checks intentionally collapse to one canonical unsafe-path error.
func ValidateLogicalPath(value string) (string, error) {
	if !utf8.ValidString(value) || len(value) < 1 || len(value) > 1024 ||
		strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") ||
		strings.Contains(value, "\\") || strings.ContainsRune(value, 0) {
		return "", ErrUnsafeLogicalPath
	}
	parts := strings.Split(value, "/")
	for index, part := range parts {
		if part == "" || part == "." || part == ".." || len(part) > 255 || hasControl(part) {
			return "", ErrUnsafeLogicalPath
		}
		if index == 0 && len(part) >= 2 && isASCIIAlpha(part[0]) && part[1] == ':' {
			return "", ErrUnsafeLogicalPath
		}
	}
	return strings.Join(parts, "/"), nil
}

func hasControl(value string) bool {
	for _, character := range value {
		if character == 0x7f || (character >= 0x01 && character <= 0x1f) {
			return true
		}
	}
	return false
}

func isASCIIAlpha(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func ASCIICaseFold(value string) string {
	buffer := []byte(value)
	for index, character := range buffer {
		if character >= 'A' && character <= 'Z' {
			buffer[index] = character + ('a' - 'A')
		}
	}
	return string(buffer)
}
