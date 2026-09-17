package libraryimport

import (
	"encoding/json"
	"fmt"
	model "retrom/internal/model/libraryimport"
	"slices"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/model/tagging"
)

// ValidateOwnedSourceRequest compares caller input with the frozen ES plan before any upload is reserved.
func ValidateOwnedSourceRequest(before model.SourceCreationSnapshot, request model.OwnedServerSourceRequest) error {
	if before.Kind != model.SourceOwnerEmulationStation {
		return nil
	}
	mode := request.ContentMode
	if mode == "" {
		mode = contentcapability.ModeStandard
	}
	expected := contentcapability.ModeStandard
	if before.Frozen.ContentKind == contentcapability.ModeMultiDisc {
		expected = contentcapability.ModeMultiDisc
	}
	if request.AssignedByUserID != before.Frozen.ActorUserID || mode != expected {
		return model.ErrInvalid
	}
	var references []tagging.Reference
	if err := json.Unmarshal([]byte(before.Frozen.TagSnapshotJSON), &references); err != nil {
		return fmt.Errorf("decode frozen source tags: %w", err)
	}
	if references == nil {
		return model.ErrVersionConflict
	}
	wanted := make([]string, 0, len(references))
	for _, reference := range references {
		wanted = append(wanted, reference.TagID)
	}
	actual := slices.Clone(request.TagIDs)
	slices.Sort(wanted)
	slices.Sort(actual)
	if !slices.Equal(wanted, actual) {
		return model.ErrInvalid
	}
	return nil
}

func validSourceFrozen(before model.SourceCreationSnapshot) bool {
	if before.Kind != model.SourceOwnerEmulationStation {
		return true
	}
	frozen := before.Frozen
	return frozen.RootID != "" && frozen.RootDigest != "" && frozen.ActorUserID != "" && frozen.CollectionID != "" &&
		frozen.MappingVersion > 0 && frozen.ReleaseYearMax >= 1950 && frozen.MaxAttempts >= before.Attempt &&
		frozen.StartedAtMS > 0 && frozen.StartedAtMS < before.DeadlineMS
}
