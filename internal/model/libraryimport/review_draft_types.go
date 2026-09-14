package libraryimport

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"retrom/internal/model/tagging"
)

// MetadataPatch contains the editable metadata fields of a review draft.
// The nullable wrappers preserve the distinction between an omitted field and
// an explicit JSON null while keeping that detail out of the persistence
// layer.
type MetadataPatch struct {
	Title       *string               `json:"title,omitempty"`
	Description *string               `json:"description,omitempty"`
	Developer   *string               `json:"developer,omitempty"`
	Publisher   *string               `json:"publisher,omitempty"`
	Genre       *string               `json:"genre,omitempty"`
	Players     optionalNullableInt64 `json:"players,omitempty"`
	ReleaseYear optionalNullableInt64 `json:"releaseYear,omitempty"`
}

type optionalNullableInt64 struct {
	present bool
	value   *int64
}

func (value optionalNullableInt64) Optional() (bool, *int64) {
	return value.present, value.value
}

func (value *optionalNullableInt64) UnmarshalJSON(contents []byte) error {
	value.present = true
	if string(contents) == "null" {
		value.value = nil
		return nil
	}
	var decoded int64
	if err := json.Unmarshal(contents, &decoded); err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	value.value = &decoded
	return nil
}

type optionalNullableString struct {
	present bool
	value   *string
}

func (value optionalNullableString) Optional() (bool, *string) {
	return value.present, value.value
}

func (value *optionalNullableString) UnmarshalJSON(contents []byte) error {
	value.present = true
	if string(contents) == "null" {
		value.value = nil
		return nil
	}
	var decoded string
	if err := json.Unmarshal(contents, &decoded); err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	value.value = &decoded
	return nil
}

type SelectedAssets struct {
	CoverCandidateAssetID       *string  `json:"coverCandidateAssetId,omitempty"`
	CoverUploadedAssetID        *string  `json:"coverUploadedAssetId,omitempty"`
	BackgroundCandidateAssetID  *string  `json:"backgroundCandidateAssetId,omitempty"`
	ScreenshotCandidateAssetIDs []string `json:"screenshotCandidateAssetIds,omitempty"`
}

type DraftPatch struct {
	ScummVMCandidateID       *string                `json:"scummvmCandidateId,omitempty"`
	TargetPlatformInstanceID *string                `json:"targetPlatformInstanceId,omitempty"`
	Metadata                 *MetadataPatch         `json:"metadata,omitempty"`
	SelectedValidationID     *string                `json:"selectedValidationId,omitempty"`
	SelectedCandidateID      optionalNullableString `json:"selectedCandidateId,omitempty"`
	SelectedAssets           *SelectedAssets        `json:"selectedAssets,omitempty"`
	DefaultDOSEntry          optionalNullableString `json:"defaultDosEntry,omitempty"`
	TagIDs                   []string               `json:"tagIds"`
	RPGSelfContainedOverride *bool                  `json:"rpgSelfContainedOverride,omitempty"`
}

type DraftResult struct {
	ItemID      string              `json:"itemId"`
	Version     int64               `json:"version"`
	Metadata    map[string]any      `json:"metadata"`
	Tags        []tagging.Reference `json:"tags"`
	UpdatedAtMS int64               `json:"updatedAtMs"`
}

// ErrReimportRequiredPlatformChange is returned when changing the base
// platform would invalidate the source grouping and identification evidence.
var ErrReimportRequiredPlatformChange = errors.New("REIMPORT_REQUIRED_FOR_PLATFORM_CHANGE")

// ValidReviewField applies the common editor field contract. It is exported
// for legacy import orchestration that still shares this validation rule.
func ValidReviewField(value string, maximum int, multiline bool) bool {
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

// ValidateDraftPatch checks request shape before opening a database
// transaction. State-dependent validation remains in the repository.
func ValidateDraftPatch(patch DraftPatch) error {
	noChange := patch.TargetPlatformInstanceID == nil && patch.Metadata == nil &&
		patch.SelectedValidationID == nil && !patch.SelectedCandidateID.present &&
		patch.SelectedAssets == nil && !patch.DefaultDOSEntry.present && len(patch.TagIDs) == 0 &&
		patch.RPGSelfContainedOverride == nil && patch.ScummVMCandidateID == nil
	if patch.TagIDs == nil || noChange {
		return ErrInvalid
	}
	return nil
}
