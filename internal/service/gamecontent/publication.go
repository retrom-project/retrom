package gamecontent

import (
	"context"
	"fmt"
	"slices"

	"retrom/internal/corevalidation"
	"retrom/internal/multidisc"
	validation "retrom/internal/service/corevalidation"
)

func (service *Service) publish(
	ctx context.Context,
	claim Claim,
	snapshot JobSnapshot,
	prepared PreparedReplacement,
) error {
	now := service.now().UnixMilli()
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		binding, err := publicationBinding(ctx, scope, claim, snapshot, prepared, now)
		if err != nil {
			return err
		}
		dependency, err := replacementDependencies(ctx, scope, snapshot, prepared, binding)
		if err != nil {
			return err
		}
		impact, err := scope.Retirements.Retire(ctx, snapshot.GameID, snapshot.VariantID, now)
		if err != nil {
			return fmt.Errorf("retire current game content: %w", err)
		}
		if err := scope.ContentWriter.Publish(
			ctx,
			Publication{
				JobID:                  claim.JobID,
				Snapshot:               snapshot,
				Prepared:               prepared,
				DependencySnapshotJSON: dependency,
				Now:                    now,
			},
		); err != nil {
			return fmt.Errorf("publish replacement content: %w", err)
		}
		if err := scope.Retirements.Stage(ctx, impact.CandidateBlobIDs); err != nil {
			return fmt.Errorf("stage replaced blob candidates: %w", err)
		}
		if err := scope.Jobs.Succeed(ctx, Outcome{
			Claim: claim, GameID: snapshot.GameID, ManifestDigest: prepared.ManifestDigest,
			VariantID: snapshot.VariantID, RetiredSaveCount: impact.SaveStateCount, Now: now,
		}); err != nil {
			return fmt.Errorf("complete replacement execution: %w", err)
		}
		if err := scope.Retirements.ReleaseUpload(ctx, claim.JobID, now); err != nil {
			return fmt.Errorf("release completed replacement upload: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit replacement publication: %w", err)
	}
	if service.payloadReleases != nil {
		service.payloadReleases.Signal()
	}
	return nil
}

func replacementDependencies(
	ctx context.Context,
	scope WriteScope,
	snapshot JobSnapshot,
	prepared PreparedReplacement,
	binding Binding,
) ([]byte, error) {
	if prepared.RPGMaker != nil {
		return []byte(binding.DependencySnapshotJSON), nil
	}
	bios, status, code, err := validation.New(
		scope.BIOS,
	).ResolveBIOS(
		ctx,
		snapshot.ProviderID,
		snapshot.TargetID,
		prepared.FirstContentLogicalName,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve replacement dependencies: %w", err)
	}
	if status != "READY" {
		return nil, &replacementValidationError{code: code}
	}
	if prepared.ContentKind == multidisc.ContentKind {
		bios.MultiDisc = &corevalidation.MultiDiscSnapshot{
			ContentKind: corevalidation.MultiDiscContentKind, ParserVersion: corevalidation.MultiDiscParserVersion,
			DiscCount: len(
				prepared.OrderedDiscSHA256,
			), MissingEntries: []corevalidation.MultiDiscMissingEntry{}, OrderedDiscSHA256: prepared.OrderedDiscSHA256,
			CanonicalPlaylistSHA256: prepared.CanonicalPlaylist.SHA256, Delivery: corevalidation.MultiDiscDelivery,
		}
	}
	result, err := bios.JSON()
	if err != nil {
		return nil, fmt.Errorf("encode replacement dependencies: %w", err)
	}
	return result, nil
}

func publicationBinding(
	ctx context.Context,
	scope WriteScope,
	claim Claim,
	snapshot JobSnapshot,
	prepared PreparedReplacement,
	now int64,
) (Binding, error) {
	current, err := scope.Leases.Current(ctx, claim, now)
	if err != nil {
		return Binding{}, fmt.Errorf("check replacement ownership: %w", err)
	}
	if !current {
		return Binding{}, ErrExecutionLost
	}
	binding, err := scope.Content.Binding(ctx, snapshot.GameID)
	if err != nil {
		return Binding{}, fmt.Errorf("check current replacement binding: %w", err)
	}
	if !replacementBindingMatchesSnapshot(binding, snapshot) ||
		pointerText(binding.DATID) != pointerText(snapshot.DATVersionID) {
		return Binding{}, &replacementValidationError{code: "GAME_CONTENT_CHANGED"}
	}
	identity, err := scope.Content.Identity(ctx, snapshot.GameID)
	if err != nil {
		return Binding{}, fmt.Errorf("read current replacement identity: %w", err)
	}
	if prepared.ContentKind == multidisc.ContentKind {
		identity = slices.DeleteFunc(identity, func(file IdentityFile) bool { return file.Role != "DISC" })
	}
	if slices.Equal(identity, preparedContentIdentity(prepared)) {
		return Binding{}, &replacementValidationError{code: "GAME_CONTENT_UNCHANGED"}
	}
	return binding, nil
}
