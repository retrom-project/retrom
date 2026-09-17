package libraryimport

import (
	"strings"
	"unicode"
	"unicode/utf8"

	model "retrom/internal/model/libraryimport"
)

const (
	reviewDescriptionMaximumRunes = 10_000
	reviewShortFieldMaximumRunes  = 200
)

func validServerSourceMetadata(metadata model.ServerMetadata) bool {
	if metadata.Title == "" || !validField(metadata.Title, 200, false) {
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
		if !validField(value.text, value.maximum, value.multiline) {
			return false
		}
	}
	if metadata.Players != nil && (*metadata.Players < 1 || *metadata.Players > 64) {
		return false
	}
	return metadata.ReleaseYear == nil || *metadata.ReleaseYear >= 1000 && *metadata.ReleaseYear <= 9999
}

func validServerReviewMetadata(metadata model.ServerMetadata, maximumYear int) bool {
	if metadata.Title == "" || !validField(metadata.Title, reviewShortFieldMaximumRunes, false) ||
		!validField(metadata.Description, reviewDescriptionMaximumRunes, true) ||
		!validField(metadata.Developer, reviewShortFieldMaximumRunes, false) ||
		!validField(metadata.Publisher, reviewShortFieldMaximumRunes, false) ||
		!validField(metadata.Genre, reviewShortFieldMaximumRunes, false) {
		return false
	}
	if metadata.Players != nil && (*metadata.Players < 1 || *metadata.Players > 64) {
		return false
	}
	return metadata.ReleaseYear == nil || *metadata.ReleaseYear >= 1950 && *metadata.ReleaseYear <= maximumYear
}

func NormalizeServerReviewMetadata(
	metadata model.ServerMetadata,
	maximumYear int,
) (model.ServerMetadata, []model.ServerMetadataWarning, error) {
	if !validServerSourceMetadata(metadata) {
		return model.ServerMetadata{}, nil, model.ErrInvalid
	}
	warnings := make([]model.ServerMetadataWarning, 0, 5)
	truncate := func(value string, maximum int, field string) string {
		runes := []rune(value)
		if len(runes) <= maximum {
			return value
		}
		warnings = append(warnings, model.ServerMetadataWarning{Code: "FIELD_TRUNCATED", Field: field})
		return string(runes[:maximum])
	}
	metadata.Description = truncate(metadata.Description, reviewDescriptionMaximumRunes, "description")
	metadata.Developer = truncate(metadata.Developer, reviewShortFieldMaximumRunes, "developer")
	metadata.Publisher = truncate(metadata.Publisher, reviewShortFieldMaximumRunes, "publisher")
	metadata.Genre = truncate(metadata.Genre, reviewShortFieldMaximumRunes, "genre")
	if metadata.ReleaseYear != nil && (*metadata.ReleaseYear < 1950 || *metadata.ReleaseYear > maximumYear) {
		metadata.ReleaseYear = nil
		warnings = append(warnings, model.ServerMetadataWarning{Code: "FIELD_VALUE_INVALID", Field: "releaseYear"})
	}
	if !validServerReviewMetadata(metadata, maximumYear) {
		return model.ServerMetadata{}, nil, model.ErrInvalid
	}
	return metadata, warnings, nil
}

func validField(value string, maximum int, multiline bool) bool {
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
