package emulationstationimport

import "math"

func startState(summary Summary, version int64) (bool, error) {
	if summary.Version != version || version < 1 {
		return false, ErrVersionConflict
	}
	switch summary.State {
	case "QUEUED", "RUNNING", "COMPLETED", "PARTIAL_FAILURE":
		return true, nil
	case "AWAITING_MAPPING":
	default:
		return false, ErrExpired
	}
	if version == math.MaxInt64 {
		return false, ErrVersionConflict
	}
	return false, nil
}

func readyToStart(before StartSnapshot, now int64) error {
	summary := before.Summary
	if now >= summary.ExpiresAtMS {
		return ErrExpired
	}
	mapped := summary.Counts.MappedCollections + summary.Counts.SkippedCollections
	if mapped != summary.Counts.Collections || !before.TagsValid {
		return ErrMapping
	}
	if summary.Counts.MappedCollections == 0 {
		return ErrNoSelection
	}
	if !before.TargetsValid {
		return ErrMappingTargetChanged
	}
	if before.OtherActive {
		return ErrActive
	}
	if summary.ImportJobID != nil {
		return ErrMapping
	}
	return nil
}
