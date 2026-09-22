package libraryimport

import (
	"encoding/json"
	"fmt"
	"strings"

	"retrom/internal/service/payloadrelease"

	"retrom/internal/authn"
	"retrom/internal/gametitle"
)

func (run *reviewApprovalRun) publish() error {
	for _, step := range []func() error{run.publishGame, run.publishVariant, run.publishDecision} {
		if err := step(); err != nil {
			return err
		}
	}
	if run.request.Bulk != nil {
		if err := run.scope.Bulk.RecordPublished(run.ctx, BulkPublication{
			Intent: *run.request.Bulk, ItemID: run.request.ItemID, Result: run.result(), NowMS: run.now,
			ReviewVersion: run.request.ExpectedVersion, LeasedUntilMS: run.now + 60_000,
		}); err != nil {
			return fmt.Errorf("record bulk published review: %w", err)
		}
	}
	return nil
}

func (run *reviewApprovalRun) publishGame() error {
	if err := run.scope.Games.CreateGame(run.ctx, ApprovalGame{
		ID: run.gameID, PlatformInstanceID: run.head.PlatformInstanceID,
		TitleInitial: gametitle.Initial(run.metadata.Title), SearchText: strings.ToLower(run.metadata.Title),
		SourceKind: run.origin.Kind, SourceRefID: run.origin.RefID, ContentKind: run.head.ContentKind,
		ManifestJSON: run.head.SourceManifestJSON, ManifestDigest: run.head.SourceManifestDigest,
		Metadata: run.metadata, NowMS: run.now,
	}); err != nil {
		return fmt.Errorf("create approved game: %w", err)
	}
	principal, _ := authn.PrincipalFromContext(run.ctx)
	tags, err := run.service.tags.CopyDraftTagsToGame(run.ctx, run.scope.Tags,
		run.head.DraftID, run.gameID, principal.UserID, run.now)
	if err != nil {
		return fmt.Errorf("publish review tags: %w", err)
	}
	run.publishedTags = tags
	for _, asset := range run.assets {
		if err := run.scope.Games.CreateAsset(run.ctx, asset); err != nil {
			return fmt.Errorf("publish review asset: %w", err)
		}
	}
	if err := run.scope.Games.CopySourceFiles(run.ctx, run.contentCopy()); err != nil {
		return fmt.Errorf("publish game source files: %w", err)
	}
	if run.head.PlatformID == "rpgmaker" {
		if err := run.scope.Games.CopyRPGProfile(run.ctx, run.contentCopy()); err != nil {
			return fmt.Errorf("publish RPG content profile: %w", err)
		}
	}
	return nil
}

func (run *reviewApprovalRun) contentCopy() ApprovalContentCopy {
	return ApprovalContentCopy{
		GameID: run.gameID, ItemID: run.request.ItemID, SnapshotID: run.head.SourceSnapshotID,
		DraftID: run.head.DraftID, NowMS: run.now,
	}
}

func (run *reviewApprovalRun) publishVariant() error {
	variant := ApprovalVariant{
		ID: run.variantID, GameID: run.gameID, CoreID: run.head.CoreID, ProviderID: run.head.ProviderID,
		TargetID: run.head.TargetID, DATID: run.head.DATID, DefaultDOS: run.head.ValidationDOS,
		CompatibilityCode: "READY", DependencyJSON: run.runtimeDependencyJSON, NowMS: run.now,
	}
	if run.head.ProviderID == "emulatorjs" {
		id, err := run.scope.Variants.NextEmulatorID(run.ctx)
		if err != nil {
			return fmt.Errorf("allocate emulator game number: %w", err)
		}
		variant.EmulatorGameID = &id
	}
	if run.head.DraftDOS != nil {
		variant.DefaultDOS = run.head.DraftDOS
	}
	if run.screenshotOverride {
		variant.CompatibilityCode = "REVIEW_SCREENSHOT_OVERRIDE"
	}
	if err := run.scope.Variants.CreateVariant(run.ctx, variant); err != nil {
		return fmt.Errorf("publish game variant: %w", err)
	}
	if err := run.scope.Variants.CopyValidationFiles(run.ctx, ApprovalValidationCopy{
		VariantID: run.variantID, ValidationID: run.head.ValidationID,
	}); err != nil {
		return fmt.Errorf("publish variant files: %w", err)
	}
	if err := run.publishDependencies(); err != nil {
		return err
	}
	if run.head.PlatformID == "rpgmaker" {
		if err := run.scope.Variants.CreateRPGVariant(run.ctx, ApprovalRPGVariant{
			VariantID: run.variantID, Generation: run.rpgProfile.Generation,
			DependencyDigest: run.rpgDependencies.Digest,
		}); err != nil {
			return fmt.Errorf("publish RPG variant profile: %w", err)
		}
	}
	if err := run.scope.Games.CopyDOSEntries(run.ctx, run.contentCopy()); err != nil {
		return fmt.Errorf("publish DOS entries: %w", err)
	}
	return nil
}

func (run *reviewApprovalRun) publishDependencies() error {
	var snapshot struct {
		Dependencies []struct {
			Kind, Machine, State string
			RequiredEntries      []string
		}
	}
	if err := json.Unmarshal([]byte(run.head.DependencyJSON), &snapshot); err != nil {
		return fmt.Errorf("decode published dependencies: %w", err)
	}
	for _, dependency := range snapshot.Dependencies {
		if run.head.DATID == nil || (dependency.Kind != "PARENT" && dependency.Kind != "BIOS_OR_BASE") {
			return ErrInvalid
		}
		entries, err := json.Marshal(dependency.RequiredEntries)
		if err != nil {
			return fmt.Errorf("encode published dependency entries: %w", err)
		}
		if err := run.scope.Variants.CreateDependency(run.ctx, ApprovalVariantDependency{
			VariantID: run.variantID, Kind: dependency.Kind, Machine: dependency.Machine,
			DATID:               *run.head.DATID,
			RequiredEntriesJSON: string(entries), State: dependency.State, NowMS: run.now,
		}); err != nil {
			return fmt.Errorf("publish variant dependency: %w", err)
		}
	}
	return nil
}

func (run *reviewApprovalRun) publishDecision() error {
	if err := run.scope.Decisions.PublishItem(run.ctx, run.publication); err != nil {
		return fmt.Errorf("publish review and aggregate: %w", err)
	}
	if err := run.scope.Decisions.TransitionOwner(run.ctx, ReviewOwnerTransition{
		ItemID: run.request.ItemID, State: ReviewOwnerPublished, GameID: &run.gameID, NowMS: run.now,
	}); err != nil {
		return fmt.Errorf("publish review source owner: %w", err)
	}
	if err := payloadrelease.NewScheduler(nil).Review(
		run.ctx,
		run.scope.Payload,
		payloadrelease.ReviewRelease{
			ItemID:   run.request.ItemID,
			ImportID: run.head.ImportID,
			Reason:   payloadrelease.ReasonImportPublished,
			NowMS:    run.now,
		},
	); err != nil {
		return fmt.Errorf("schedule approved payload release: %w", err)
	}
	return nil
}
