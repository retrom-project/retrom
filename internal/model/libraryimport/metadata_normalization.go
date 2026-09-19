package libraryimport

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ReviewDescriptionMaximumRunes = 10_000
	ReviewShortFieldMaximumRunes  = 200
)

func ValidServerSourceMetadata(metadata ServerMetadata) bool {
	if metadata.Title == "" || !ValidField(metadata.Title, 200, false) {
		return false
	}
	for _, value := range []struct {
		text      string
		maximum   int
		multiline bool
	}{
		{metadata.Description, 20_000, true},
		{metadata.Developer, 500, false},
		{metadata.Publisher, 500, false},
		{metadata.Genre, 500, false},
	} {
		if !ValidField(value.text, value.maximum, value.multiline) {
			return false
		}
	}
	if metadata.Players != nil && (*metadata.Players < 1 || *metadata.Players > 64) {
		return false
	}
	return metadata.ReleaseYear == nil || *metadata.ReleaseYear >= 1000 && *metadata.ReleaseYear <= 9999
}

func ValidServerReviewMetadata(metadata ServerMetadata, maximumYear int) bool {
	if metadata.Title == "" || !ValidField(metadata.Title, ReviewShortFieldMaximumRunes, false) ||
		!ValidField(metadata.Description, ReviewDescriptionMaximumRunes, true) ||
		!ValidField(metadata.Developer, ReviewShortFieldMaximumRunes, false) ||
		!ValidField(metadata.Publisher, ReviewShortFieldMaximumRunes, false) ||
		!ValidField(metadata.Genre, ReviewShortFieldMaximumRunes, false) {
		return false
	}
	if metadata.Players != nil && (*metadata.Players < 1 || *metadata.Players > 64) {
		return false
	}
	return metadata.ReleaseYear == nil || *metadata.ReleaseYear >= 1950 && *metadata.ReleaseYear <= maximumYear
}

// NormalizeServerReviewMetadata validates and normalizes server review metadata.
func NormalizeServerReviewMetadata(
	metadata ServerMetadata,
	maximumYear int,
) (ServerMetadata, []ServerMetadataWarning, error) {
	if !ValidServerSourceMetadata(metadata) {
		return ServerMetadata{}, nil, ErrInvalid
	}
	warnings := make([]ServerMetadataWarning, 0, 5)
	truncate := func(value string, maximum int, field string) string {
		runes := []rune(value)
		if len(runes) <= maximum {
			return value
		}
		warnings = append(warnings, ServerMetadataWarning{Code: "FIELD_TRUNCATED", Field: field})
		return string(runes[:maximum])
	}
	metadata.Description = truncate(metadata.Description, ReviewDescriptionMaximumRunes, "description")
	metadata.Developer = truncate(metadata.Developer, ReviewShortFieldMaximumRunes, "developer")
	metadata.Publisher = truncate(metadata.Publisher, ReviewShortFieldMaximumRunes, "publisher")
	metadata.Genre = truncate(metadata.Genre, ReviewShortFieldMaximumRunes, "genre")
	if metadata.ReleaseYear != nil && (*metadata.ReleaseYear < 1950 || *metadata.ReleaseYear > maximumYear) {
		metadata.ReleaseYear = nil
		warnings = append(warnings, ServerMetadataWarning{Code: "FIELD_VALUE_INVALID", Field: "releaseYear"})
	}
	if !ValidServerReviewMetadata(metadata, maximumYear) {
		return ServerMetadata{}, nil, ErrInvalid
	}
	return metadata, warnings, nil
}

// ValidField checks whether a text field is valid according to encoding, trim and length rules.
func ValidField(value string, maximum int, multiline bool) bool {
	if !utf8.ValidString(value) || value != strings.TrimSpace(value) || utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) &&
			(!multiline || (character != '\n' && character != '\r' && character != '\t')) {
			return false
		}
	}
	return true
}
