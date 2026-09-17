package emulationstationimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/emulationstationimport"
)

func verifyFrozenSource(
	ctx context.Context,
	sources model.FrozenSources,
	summary model.Summary,
	before model.FrozenSourceSnapshot,
) error {
	if before.SourceSnapshotDigest == "" || !validFrozenEvidence(before.Gamelists) {
		return model.ErrSourceChanged
	}
	root, err := sources.Select(ctx, summary.Root.ID, summary.SourceRelativePath)
	if err != nil {
		return fmt.Errorf("%w: %w", model.ErrSourceChanged, err)
	}
	if root.ID != summary.Root.ID || root.Digest != before.RootConfigDigest {
		return model.ErrSourceChanged
	}
	if err := sources.VerifyGamelists(
		ctx, root.ID, summary.SourceRelativePath, before.Gamelists,
	); err != nil {
		return fmt.Errorf("verify EmulationStation frozen evidence: %w", err)
	}
	return nil
}

func validFrozenEvidence(values []model.GamelistEvidence) bool {
	if len(values) == 0 || len(values) > model.MaxSnapshotGamelists {
		return false
	}
	var total int64
	for _, value := range values {
		if value.SizeBytes < 0 || value.RelativePath == "" || !validFrozenDigest(value.FactsDigest) {
			return false
		}
		if value.ContentDigest == nil {
			if value.ParseState != "INVALID" || value.SizeBytes <= model.MaxSnapshotGamelistBytes {
				return false
			}
			continue
		}
		if value.SizeBytes > model.MaxSnapshotGamelistBytes || !validFrozenDigest(*value.ContentDigest) ||
			value.ParseState != "VALID" &&
				value.ParseState != "INVALID" || total > model.MaxSnapshotGamelistsBytes-value.SizeBytes {
			return false
		}
		total += value.SizeBytes
	}
	return true
}

func validFrozenDigest(value string) bool {
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

func sameFrozenSource(beforeSummary, currentSummary model.Summary, before, current model.FrozenSourceSnapshot) bool {
	if before.RootConfigDigest != current.RootConfigDigest ||
		before.SourceSnapshotDigest != current.SourceSnapshotDigest ||
		before.ReleaseYearMax != current.ReleaseYearMax || beforeSummary.Root.ID != currentSummary.Root.ID ||
		beforeSummary.SourceRelativePath != currentSummary.SourceRelativePath ||
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
