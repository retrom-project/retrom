package libraryimport

import (
	"context"
	"fmt"
	"math"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/content/multidisc"
	model "retrom/internal/model/libraryimport"
	validation "retrom/internal/service/corevalidation"
)

func ValidateApprovalDependencies(
	ctx context.Context, scope model.ApprovalDependencyScope, input model.ApprovalDependencyInput,
) error {
	snapshot, err := corevalidation.ParseSnapshot(input.DependencyJSON)
	if err != nil {
		if input.PlatformID != "arcade" || input.ContentKind != "SINGLE_FILE" {
			return model.ErrInvalid
		}
		return validateApprovalArcade(ctx, scope, input.ValidationID, input.DependencyJSON)
	}
	name, err := scope.Reader.LogicalName(ctx, input.SnapshotID)
	if err != nil {
		return fmt.Errorf("read approval content name: %w", err)
	}
	current, status, _, err := validation.New(scope.BIOS).ResolveBIOS(ctx, input.ProviderID, input.TargetID, name)
	if err != nil {
		return fmt.Errorf("resolve current approval BIOS: %w", err)
	}
	if status != "READY" {
		return model.ErrInvalid
	}
	current.MultiDisc = snapshot.MultiDisc
	encoded, err := current.JSON()
	if err != nil {
		return fmt.Errorf("encode current approval dependencies: %w", err)
	}
	if string(encoded) != input.DependencyJSON {
		return model.ErrInvalid
	}
	if input.ContentKind != multidisc.ContentKind {
		return nil
	}
	return validateApprovalMultiDisc(ctx, scope.Reader, input, snapshot)
}

func validateApprovalMultiDisc(
	ctx context.Context, reader model.ApprovalDependencyReader, input model.ApprovalDependencyInput,
	snapshot corevalidation.Snapshot,
) error {
	capabilities := contentcapability.Resolve(input.PlatformID, true, true, input.Policy)
	if capabilities.MultiDisc == nil || snapshot.MultiDisc == nil || len(snapshot.MultiDisc.MissingEntries) != 0 {
		return model.ErrInvalid
	}
	facts, err := reader.MultiDisc(ctx, input.SnapshotID, input.ValidationID)
	if err != nil {
		return fmt.Errorf("read approval discs: %w", err)
	}
	total, err := approvalDiscTotal(facts.Discs)
	if err != nil {
		return err
	}
	count := len(facts.Discs)
	if count < 2 || count > capabilities.MultiDisc.MaxDiscs || count != snapshot.MultiDisc.DiscCount ||
		total > capabilities.MultiDisc.MaxTotalBytes {
		return model.ErrInvalid
	}
	if facts.PlaylistCount != 1 || facts.DiscCount != int64(count) || facts.SourceCount != int64(count)+1 ||
		facts.CanonicalCount != 1 {
		return model.ErrInvalid
	}
	return nil
}

func approvalDiscTotal(discs []model.ApprovalDisc) (int64, error) {
	var total int64
	for ordinal, disc := range discs {
		if disc.Ordinal != ordinal || disc.State != "PRESENT" || disc.SourceOrdinal == nil ||
			*disc.SourceOrdinal != int64(ordinal) || disc.SourceBlobID == nil || disc.BlobID == nil ||
			*disc.SourceBlobID != *disc.BlobID ||
			disc.SourceLogicalName == nil || *disc.SourceLogicalName != disc.LogicalName ||
			disc.SizeBytes == nil || *disc.SizeBytes < 8 {
			return 0, model.ErrInvalid
		}
		if total > math.MaxInt64-*disc.SizeBytes {
			return 0, model.ErrInvalid
		}
		total += *disc.SizeBytes
	}
	return total, nil
}
