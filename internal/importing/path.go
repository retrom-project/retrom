package importing

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrUnsafeLogicalPath = errors.New("UNSAFE_LOGICAL_PATH")

func ValidateLogicalPath(value string) (string, error) {
	if unsafeWholePath(value) {
		return "", ErrUnsafeLogicalPath
	}
	parts := strings.Split(value, "/")
	for index, part := range parts {
		if unsafePathPart(part) || index == 0 && windowsDrivePart(part) {
			return "", ErrUnsafeLogicalPath
		}
	}
	return strings.Join(parts, "/"), nil
}

func unsafeWholePath(value string) bool {
	return !utf8.ValidString(value) || len(value) < 1 || len(value) > 1024 ||
		strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") ||
		strings.Contains(value, "\\") || strings.ContainsRune(value, 0)
}

func unsafePathPart(part string) bool {
	return part == "" || part == "." || part == ".." || len(part) > 255 || hasControl(part)
}

func windowsDrivePart(part string) bool {
	return len(part) >= 2 && isASCIIAlpha(part[0]) && part[1] == ':'
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
