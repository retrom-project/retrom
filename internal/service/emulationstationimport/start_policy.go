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

func validStartEvidence(values []GamelistEvidence) bool {
	if len(values) == 0 || len(values) > MaxStartGamelists {
		return false
	}
	var total int64
	for _, value := range values {
		if value.SizeBytes < 0 || value.RelativePath == "" || !validStartDigest(value.FactsDigest) {
			return false
		}
		if value.ContentDigest == nil {
			if value.ParseState != "INVALID" || value.SizeBytes <= MaxStartGamelistBytes {
				return false
			}
			continue
		}
		if value.SizeBytes > MaxStartGamelistBytes || !validStartDigest(*value.ContentDigest) ||
			value.ParseState != "VALID" && value.ParseState != "INVALID" || total > MaxStartGamelistsBytes-value.SizeBytes {
			return false
		}
		total += value.SizeBytes
	}
	return true
}

func validStartDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' && ch < 'a' || ch > 'f' {
			return false
		}
	}
	return true
}

func sameStartSnapshot(before, current StartSnapshot) bool {
	if before.RootConfigDigest != current.RootConfigDigest ||
		before.SourceSnapshotDigest != current.SourceSnapshotDigest ||
		before.ReleaseYearMax != current.ReleaseYearMax || before.Summary.Root.ID != current.Summary.Root.ID ||
		before.Summary.SourceRelativePath != current.Summary.SourceRelativePath ||
		len(before.Gamelists) != len(current.Gamelists) {
		return false
	}
	for i, left := range before.Gamelists {
		right := current.Gamelists[i]
		if left.RelativePath != right.RelativePath || left.FactsDigest != right.FactsDigest ||
			left.ParseState != right.ParseState || left.SizeBytes != right.SizeBytes {
			return false
		}
		if (left.ContentDigest == nil) != (right.ContentDigest == nil) {
			return false
		}
		if left.ContentDigest != nil && *left.ContentDigest != *right.ContentDigest {
			return false
		}
	}
	return true
}
