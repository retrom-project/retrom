package emulationstationimport

import (
	"math"

	model "retrom/internal/model/emulationstationimport"
)

func startState(summary model.Summary, version int64) (bool, error) {
	if summary.Version != version || version < 1 {
		return false, model.ErrVersionConflict
	}
	switch summary.State {
	case "QUEUED", "RUNNING", "COMPLETED", "PARTIAL_FAILURE":
		return true, nil
	case "AWAITING_MAPPING":
	default:
		return false, model.ErrExpired
	}
	if version == math.MaxInt64 {
		return false, model.ErrVersionConflict
	}
	return false, nil
}

func readyToStart(before model.StartSnapshot, now int64) error {
	summary := before.Summary
	if now >= summary.ExpiresAtMS {
		return model.ErrExpired
	}
	mapped := summary.Counts.MappedCollections + summary.Counts.SkippedCollections
	if mapped != summary.Counts.Collections || !before.TagsValid {
		return model.ErrMapping
	}
	if summary.Counts.MappedCollections == 0 {
		return model.ErrNoSelection
	}
	if !before.TargetsValid {
		return model.ErrMappingTargetChanged
	}
	if before.OtherActive {
		return model.ErrActive
	}
	if summary.ImportJobID != nil {
		return model.ErrMapping
	}
	return nil
}
