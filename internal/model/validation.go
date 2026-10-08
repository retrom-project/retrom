package model

import (
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

func Text(value string, maximum int) bool {
	return strings.TrimSpace(value) != "" && BoundedText(value, maximum)
}

func Hash(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func OneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if candidate == value {
			return true
		}
	}
	return false
}

func NameKey(value string) string { return strings.ToLower(strings.Join(strings.Fields(value), " ")) }

func UUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func ValidateGameFields(input GameInput) error {
	if !UUID(input.PlatformInstanceID) || !Text(input.Title, 300) {
		return ErrInvalid
	}
	for _, field := range []struct {
		value   string
		maximum int
	}{
		{input.Description, 20000}, {input.Developer, 300}, {input.Publisher, 300}, {input.Genre, 200},
	} {
		if !BoundedText(field.value, field.maximum) {
			return ErrInvalid
		}
	}
	if input.Players != nil && !BoundedText(*input.Players, 64) {
		return ErrInvalid
	}
	if input.ReleaseYear != nil && (*input.ReleaseYear < 0 || *input.ReleaseYear > 9999) {
		return ErrInvalid
	}
	return ValidateTagIDs(input.TagIDs)
}

func BoundedText(value string, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum && !strings.ContainsRune(value, 0)
}

func ValidateTagIDs(ids []string) error {
	if len(ids) > 20 {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !UUID(id) || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
	}
	return nil
}
