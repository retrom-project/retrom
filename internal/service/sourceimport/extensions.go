package sourceimport

import (
	"sort"
	"strings"
)

// NormalizeExtensionFilter accepts explicit suffixes, never globs or paths.
func NormalizeExtensionFilter(format, input string) (string, error) {
	if format != "BASIC" && format != "PEGASUS" && format != "GAMELIST" {
		return "", ErrInvalid
	}
	if len(input) > 2048 {
		return "", ErrInvalid
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return "", nil
	}
	if format != "BASIC" {
		return "", ErrInvalid
	}
	seen := map[string]bool{}
	for _, value := range strings.Split(input, ";") {
		value = strings.ToLower(strings.TrimSpace(value))
		if !validExtension(value) {
			return "", ErrInvalid
		}
		seen[value] = true
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Strings(values)
	return strings.Join(values, ";"), nil
}

func validExtension(value string) bool {
	if len(value) < 2 || value[0] != '.' {
		return false
	}
	for _, char := range value[1:] {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9', char == '_', char == '-':
		default:
			return false
		}
	}
	return true
}
