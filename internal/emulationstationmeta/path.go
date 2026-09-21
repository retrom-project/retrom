package emulationstationmeta

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// NormalizeDeclaredPath applies only the format-level EmulationStation path
// rules. It deliberately does not inspect or canonicalize a host filesystem.
func NormalizeDeclaredPath(value string) (string, error) {
	if invalidDeclaredInput(value) {
		return "", ErrPathInvalid
	}
	normalized := strings.ReplaceAll(value, "\\", "/")
	for strings.HasPrefix(normalized, "./") {
		normalized = strings.TrimPrefix(normalized, "./")
	}
	if invalidNormalizedPrefix(normalized) || !validSegments(normalized) {
		return "", ErrPathInvalid
	}
	return normalized, nil
}

func invalidDeclaredInput(value string) bool {
	return !utf8.ValidString(value) || value == "" || len(value) > MaxDeclaredPathBytes ||
		hasBoundarySpaceOrControl(value) || containsControl(value)
}

func invalidNormalizedPrefix(value string) bool {
	return value == "" || len(value) > MaxDeclaredPathBytes || strings.HasPrefix(value, "/") ||
		strings.HasPrefix(value, "~/") || looksLikeDrivePath(value) || hasURIScheme(value)
}

func validSegments(value string) bool {
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." || len(segment) > MaxDeclaredSegmentBytes {
			return false
		}
	}
	return true
}

func hasBoundarySpaceOrControl(value string) bool {
	first, _ := utf8.DecodeRuneInString(value)
	last, _ := utf8.DecodeLastRuneInString(value)
	return unicode.IsSpace(first) || unicode.IsControl(first) || unicode.IsSpace(last) || unicode.IsControl(last)
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func looksLikeDrivePath(value string) bool {
	if len(value) < 2 || value[1] != ':' {
		return false
	}
	return value[0] >= 'a' && value[0] <= 'z' || value[0] >= 'A' && value[0] <= 'Z'
}

func hasURIScheme(value string) bool {
	colon := strings.IndexByte(value, ':')
	if colon <= 0 || !isASCIILetter(value[0]) {
		return false
	}
	for index := 1; index < colon; index++ {
		if !isSchemeCharacter(value[index]) {
			return false
		}
	}
	return true
}

func isASCIILetter(character byte) bool {
	return character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
}

func isSchemeCharacter(character byte) bool {
	return isASCIILetter(character) || character >= '0' && character <= '9' ||
		character == '+' || character == '.' || character == '-'
}
