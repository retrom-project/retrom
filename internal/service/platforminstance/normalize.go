package platforminstance

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	model "retrom/internal/model/platforminstance"
)

func SlugBase(name, platformID string) string {
	toSlug := func(value string) string {
		var builder strings.Builder
		separator := false
		for _, character := range value {
			if character >= 'A' && character <= 'Z' {
				character += 'a' - 'A'
			}
			if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
				if separator && builder.Len() > 0 && builder.Len() < 80 {
					builder.WriteByte('-')
				}
				separator = false
				if builder.Len() < 80 {
					builder.WriteRune(character)
				}
				continue
			}
			separator = builder.Len() > 0
		}
		return strings.TrimRight(builder.String(), "-")
	}
	if slug := toSlug(name); slug != "" {
		return slug
	}
	prefix := toSlug(platformID)
	if prefix == "" {
		prefix = "game"
	}
	return prefix + "-library"
}

func SlugWithSuffix(base string, suffix int) string {
	if suffix < 2 {
		return base
	}
	ending := "-" + strconv.Itoa(suffix)
	prefix := strings.TrimRight(base[:min(len(base), 80-len(ending))], "-")
	return prefix + ending
}

// NextSlug selects an unused slug from the repository's reserved names.
func NextSlug(base string, slugs []string) (string, error) {
	used := make(map[string]struct{}, len(slugs))
	for _, slug := range slugs {
		used[slug] = struct{}{}
	}
	for suffix := 1; suffix <= len(used)+1; suffix++ {
		candidate := SlugWithSuffix(base, suffix)
		if _, exists := used[candidate]; !exists {
			return candidate, nil
		}
	}
	return "", model.ErrSlugExhausted
}

func validText(value string, minimum, maximum int, allowNewline bool) bool {
	if !utf8.ValidString(value) || value != strings.TrimSpace(value) {
		return false
	}
	count := 0
	for _, character := range value {
		if unicode.IsControl(character) &&
			(!allowNewline || character != '\n' && character != '\r' && character != '\t') {
			return false
		}
		count++
	}
	return count >= minimum && count <= maximum
}
