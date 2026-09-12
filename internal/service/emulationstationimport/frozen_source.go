package emulationstationimport

import (
	"context"
	"fmt"
)

const (
	MaxSnapshotGamelists            = 1000
	MaxSnapshotGamelistBytes  int64 = 8 << 20
	MaxSnapshotGamelistsBytes int64 = 64 << 20
)

type GamelistEvidence struct {
	RelativePath, FactsDigest, ParseState string
	ContentDigest                         *string
	SizeBytes                             int64
}
type FrozenSourceSnapshot struct {
	RootConfigDigest, SourceSnapshotDigest string
	ReleaseYearMax                         int
	Gamelists                              []GamelistEvidence
}
type FrozenSources interface {
	Select(context.Context, string, string) (SelectedRoot, error)
	VerifyGamelists(context.Context, string, string, []GamelistEvidence) error
}

func verifyFrozenSource(
	ctx context.Context,
	sources FrozenSources,
	summary Summary,
	before FrozenSourceSnapshot,
) error {
	if before.SourceSnapshotDigest == "" || !validFrozenEvidence(before.Gamelists) {
		return ErrSourceChanged
	}
	root, err := sources.Select(ctx, summary.Root.ID, summary.SourceRelativePath)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSourceChanged, err)
	}
	if root.ID != summary.Root.ID || root.Digest != before.RootConfigDigest {
		return ErrSourceChanged
	}
	if err := sources.VerifyGamelists(
		ctx, root.ID, summary.SourceRelativePath, before.Gamelists,
	); err != nil {
		return fmt.Errorf("verify EmulationStation frozen evidence: %w", err)
	}
	return nil
}

func validFrozenEvidence(values []GamelistEvidence) bool {
	if len(values) == 0 || len(values) > MaxSnapshotGamelists {
		return false
	}
	var total int64
	for _, value := range values {
		if value.SizeBytes < 0 || value.RelativePath == "" || !validFrozenDigest(value.FactsDigest) {
			return false
		}
		if value.ContentDigest == nil {
			if value.ParseState != "INVALID" || value.SizeBytes <= MaxSnapshotGamelistBytes {
				return false
			}
			continue
		}
		if value.SizeBytes > MaxSnapshotGamelistBytes || !validFrozenDigest(*value.ContentDigest) ||
			value.ParseState != "VALID" && value.ParseState != "INVALID" || total > MaxSnapshotGamelistsBytes-value.SizeBytes {
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

func sameFrozenSource(beforeSummary, currentSummary Summary, before, current FrozenSourceSnapshot) bool {
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
