package libraryimport

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"retrom/internal/model/tagging"
)

// ReviewDraftPatchQuery identifies the facts needed to plan a draft patch.
// TagIDs are included so the repository can return one consistent read view of
// both the draft's current relations and the requested active tags.
type ReviewDraftPatchQuery struct {
	ItemID          string
	ExpectedVersion int64
	TagIDs          []string
}

// ReviewDraftPatchSnapshot is a storage-neutral read view. Nullable database
// values are represented as pointers and SQL/transaction types never cross
// this boundary.
type ReviewDraftPatchSnapshot struct {
	ItemID, DraftID, TargetID, EffectiveSnapshotID                string
	ValidationID                                                  string
	CandidateID, CoverID, UploadedCoverID, BackgroundID, DOSEntry *string
	Metadata                                                      map[string]any
	Version                                                       int64
	IsRPG                                                         bool
	BeforeTags, ActiveTags                                        []tagging.Reference
	SourcePaths, ScreenshotAssetIDs                               []string
}

// ReviewValidationPlan contains either an existing validation to select or a
// new validation record and its file-copy instructions. It is a value plan;
// the repo applies it inside the draft write transaction.
type ReviewValidationPlan struct {
	SelectedValidationID string
	Create               *ReviewValidationRefreshCreate
	Copy                 *ReviewValidationRefreshFileCopy
	RPGDependencyDigest  string
	Guard                ReviewValidationGuard
}

// ReviewDraftWritePlan is the complete application decision handed to the
// repository. It contains business values and guards only; it must not grow
// SQL, executors, transactions, callbacks, or service objects.
type ReviewDraftWritePlan struct {
	ItemID, DraftID string

	// These fields describe the facts used to build the plan. CommitPatch
	// checks them again inside its write transaction before applying any
	// change, so a plan cannot silently cross a changed draft snapshot.
	ExpectedVersion             int64
	ExpectedTargetID            string
	ExpectedValidationID        string
	ExpectedEffectiveSnapshotID string
	ExpectedDOSEntry            *string
	ExpectedIsRPG               bool
	ExpectedValidationGuard     ReviewValidationGuard
	ValidationSelectionExplicit bool

	TargetID, ValidationID                                        string
	CandidateID, CoverID, UploadedCoverID, BackgroundID, DOSEntry *string
	Metadata                                                      map[string]any
	SearchParts                                                   []string
	ScreenshotAssetIDs                                            []string
	AssetsChanged                                                 bool
	Tags                                                          tagging.ReplacementPlan
	ActorKind                                                     string
	ActorUserID, ActorLabel                                       *string
	RPGSelfContainedOverride                                      *bool
	RPGDependencyDigest                                           string
	ValidationCreate                                              *ReviewValidationRefreshCreate
	ValidationCopy                                                *ReviewValidationRefreshFileCopy
	NowMS                                                         int64
}

// MergeMetadata applies the editor's tri-state patch to an already decoded
// metadata document. Storage validation remains in the repository, while
// field shape and deterministic value construction stay reusable and pure.
func MergeMetadata(current map[string]any, patch *MetadataPatch, now time.Time) (map[string]any, error) {
	result := make(map[string]any, len(current))
	for key, value := range current {
		result[key] = value
	}
	if patch == nil {
		return result, nil
	}
	if err := applyTitlePatch(result, patch.Title); err != nil {
		return nil, err
	}
	if err := applyTextPatches(result, patch); err != nil {
		return nil, err
	}
	if err := applyNumericPatches(result, patch, now); err != nil {
		return nil, err
	}
	return result, nil
}

func applyTitlePatch(metadata map[string]any, value *string) error {
	if value == nil {
		return nil
	}
	if !ValidReviewField(*value, 200, false) || *value == "" {
		return ErrInvalid
	}
	metadata["title"] = *value
	return nil
}

func applyTextPatches(metadata map[string]any, patch *MetadataPatch) error {
	fields := []struct {
		key       string
		value     *string
		maximum   int
		multiline bool
	}{
		{key: "description", value: patch.Description, maximum: 10_000, multiline: true},
		{key: "developer", value: patch.Developer, maximum: 200},
		{key: "publisher", value: patch.Publisher, maximum: 200},
		{key: "genre", value: patch.Genre, maximum: 200},
	}
	for _, field := range fields {
		if field.value == nil {
			continue
		}
		if !ValidReviewField(*field.value, field.maximum, field.multiline) {
			return ErrInvalid
		}
		metadata[field.key] = *field.value
	}
	return nil
}

func applyNumericPatches(metadata map[string]any, patch *MetadataPatch, now time.Time) error {
	if present, value := patch.Players.Optional(); present {
		if value != nil && (*value < 1 || *value > 64) {
			return ErrInvalid
		}
		metadata["players"] = nullableInt(value)
	}
	if present, value := patch.ReleaseYear.Optional(); present {
		maximumYear := int64(now.UTC().Year() + 1)
		if value != nil && (*value < 1950 || *value > maximumYear) {
			return ErrInvalid
		}
		metadata["releaseYear"] = nullableInt(value)
	}
	return nil
}

func nullableInt(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

// SearchParts returns the deterministic search input for a draft snapshot.
func SearchParts(itemID string, sourcePaths []string, metadata map[string]any) []string {
	parts := make([]string, 0, 1+len(sourcePaths)+1)
	parts = append(parts, itemID)
	parts = append(parts, sourcePaths...)
	if title, ok := metadata["title"].(string); ok {
		parts = append(parts, title)
	}
	return parts
}

func EncodeMetadata(metadata map[string]any) ([]byte, error) {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/review: %w", err)
	}
	return encoded, nil
}

func SearchText(parts []string) string { return strings.ToLower(strings.Join(parts, " ")) }
