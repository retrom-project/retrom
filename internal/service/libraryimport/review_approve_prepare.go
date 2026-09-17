package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/engine/scummvm"
	"retrom/internal/model/importprogress"
	"retrom/internal/model/tagging"
)

type reviewApprovalRun struct {
	ctx                        context.Context
	service                    *ReviewApprovals
	scope                      model.ReviewApprovalScope
	request                    model.ReviewApprovalRequest
	head                       model.ReviewApprovalHead
	metadata                   model.ApprovalMetadata
	origin                     model.ApprovalOrigin
	assets                     []model.ApprovalAsset
	screenshotIDs              []string
	gameID, variantID, eventID string
	now                        int64
	screenshotOverride         bool
	runtimeDependencyJSON      string
	rpgProfile                 model.RPGReviewProfile
	rpgDependencies            model.RPGReviewDependencies
	publication                model.ApprovalPublication
	publishedTags              []tagging.Reference
	duplicateGames             []model.DuplicateGame
}

func (run *reviewApprovalRun) result() model.ReviewApproved {
	return model.ReviewApproved{GameID: run.gameID, EventID: run.eventID, Status: "PUBLISHED"}
}

func (run *reviewApprovalRun) load() error {
	head, found, err := run.scope.Reader.Head(run.ctx, run.request.ItemID)
	if err != nil {
		return fmt.Errorf("read approval evidence: %w", err)
	}
	if !found || head.State != "REVIEW_PENDING" || head.DraftVersion != run.request.ExpectedVersion ||
		head.SourceBusy {
		return model.ErrInvalid
	}
	if bulk := run.request.Bulk; bulk != nil && (head.ValidationStatus != "READY" ||
		head.ValidationID != bulk.ValidationID || head.SourceSnapshotID != bulk.SourceSnapshotID) {
		return model.ErrInvalid
	}
	run.head = head
	return nil
}

func (run *reviewApprovalRun) prepare() error {
	if !run.head.Policy.Supports(run.head.ContentKind) {
		return model.ErrInvalid
	}
	if err := json.Unmarshal([]byte(run.head.MetadataJSON), &run.metadata); err != nil {
		return fmt.Errorf("decode approval metadata: %w", err)
	}
	run.metadata.Title = strings.TrimSpace(run.metadata.Title)
	if run.metadata.Title == "" {
		return model.ErrInvalid
	}
	for _, step := range []func() error{run.prepareValidation, run.prepareOrigin, run.prepareAssets} {
		if err := step(); err != nil {
			return err
		}
	}
	run.now = run.service.now().UnixMilli()
	if err := run.projectAggregate(); err != nil {
		return err
	}
	return run.allocateIDs()
}

func (run *reviewApprovalRun) prepareValidation() error {
	current, err := NewReviewValidation(run.scope.Validation).Current(run.ctx, run.head.ValidationID)
	if err != nil {
		return fmt.Errorf("read current approval validation: %w", err)
	}
	if !current {
		return fmt.Errorf("approval validation is no longer current: %w", model.ErrInvalid)
	}
	run.runtimeDependencyJSON = run.head.DependencyJSON
	if run.head.PlatformID == "rpgmaker" {
		return run.prepareRPG()
	}
	if run.head.ContentKind == scummvm.ContentKind {
		return run.prepareScummVM()
	}
	run.screenshotOverride = run.head.ValidationStatus != "READY" && run.head.ScreenshotID != nil
	if !run.screenshotOverride {
		return ValidateApprovalDependencies(run.ctx, run.scope.Dependencies, model.ApprovalDependencyInput{
			SnapshotID: run.head.SourceSnapshotID, ValidationID: run.head.ValidationID,
			PlatformID: run.head.PlatformID,
			ProviderID: run.head.ProviderID, TargetID: run.head.TargetID, Policy: run.head.Policy,
			ContentKind: run.head.ContentKind, DependencyJSON: run.head.DependencyJSON,
		})
	}
	return run.prepareScreenshotOverride()
}

func (run *reviewApprovalRun) prepareRPG() error {
	profile, found, err := run.scope.Validation.Profile(run.ctx, run.head.DraftID)
	if err != nil {
		return fmt.Errorf("read approval RPG profile: %w", err)
	}
	if !found {
		return model.ErrInvalid
	}
	dependencies, err := model.ResolveRPGReviewDependencies(profile)
	if err != nil {
		return err
	}
	if dependencies.Status != "READY" || dependencies.SnapshotJSON != run.head.DependencyJSON ||
		profile.DependencySHA256 != dependencies.Digest {
		return model.ErrInvalid
	}
	run.rpgProfile, run.rpgDependencies = profile, dependencies
	return nil
}

func (run *reviewApprovalRun) prepareScummVM() error {
	snapshot, err := scummvm.ParseSnapshot(run.head.DependencyJSON)
	if err != nil {
		return fmt.Errorf("decode approval ScummVM snapshot: %w", err)
	}
	if run.head.ValidationStatus != "READY" || snapshot.Detection.SourceDigest != run.head.SourceManifestDigest {
		return model.ErrInvalid
	}
	if _, err := snapshot.Selected(); err != nil {
		return model.ErrInvalid
	}
	return nil
}

func (run *reviewApprovalRun) prepareScreenshotOverride() error {
	snapshot, parsed := approvalStaticSnapshot(run.head.DependencyJSON)
	if !parsed {
		return nil
	}
	filtered := make([]corevalidation.BIOSDependency, 0, len(snapshot.BIOS))
	for _, dependency := range snapshot.BIOS {
		if dependency.DeliveryKind != "EXTERNAL_FILE" ||
			(dependency.EmulatorPath != nil && dependency.BlobID != nil &&
				dependency.InstallationStatus != nil && corevalidation.BIOSInstallationUsable(*dependency.InstallationStatus)) {
			filtered = append(filtered, dependency)
		}
	}
	snapshot.BIOS = filtered
	encoded, err := snapshot.JSON()
	if err != nil {
		return fmt.Errorf("encode approval screenshot runtime: %w", err)
	}
	run.runtimeDependencyJSON = string(encoded)
	return nil
}

func (run *reviewApprovalRun) projectAggregate() error {
	if run.head.ParentVersion < 1 || run.head.ParentVersion == math.MaxInt64 ||
		run.head.Progress.Counts.ReviewPending < 1 {
		return fmt.Errorf("approval parent cannot transition: %w", model.ErrInvalid)
	}
	progress := run.head.Progress
	progress.Started = true
	progress.Counts.ReviewPending--
	projection, err := importprogress.Project(progress, run.now)
	if err != nil {
		return fmt.Errorf("project approved import progress: %w", err)
	}
	run.publication = model.ApprovalPublication{
		ItemID: run.request.ItemID, ImportID: run.head.ImportID, SnapshotID: run.head.SourceSnapshotID,
		PlatformInstanceID: run.head.PlatformInstanceID, ExpectedDraftVersion: run.head.DraftVersion,
		ExpectedParentVersion: run.head.ParentVersion, ExpectedPending: run.head.Progress.Counts.ReviewPending,
		Projection: projection, NowMS: run.now,
	}
	return nil
}

func (run *reviewApprovalRun) allocateIDs() error {
	for _, destination := range []*string{&run.gameID, &run.variantID, &run.eventID} {
		id, err := run.service.newID()
		if err != nil {
			return fmt.Errorf("allocate approval identity: %w", err)
		}
		*destination = id
	}
	for index := range run.assets {
		id, err := run.service.newID()
		if err != nil {
			return fmt.Errorf("allocate approved asset identity: %w", err)
		}
		run.assets[index].ID, run.assets[index].GameID, run.assets[index].NowMS = id, run.gameID, run.now
	}
	return nil
}

func (run *reviewApprovalRun) claimDuplicates() error {
	duplicates := model.NewContentDuplicates(run.scope.Duplicates)
	digest, err := duplicates.Identity(run.ctx, run.request.ItemID)
	if err != nil {
		return fmt.Errorf("read approval content identity: %w", err)
	}
	if err := run.scope.Decisions.ClaimIdentity(run.ctx, run.head.PlatformID, digest, run.now); err != nil {
		return fmt.Errorf("claim approval content identity: %w", err)
	}
	run.duplicateGames, err = duplicates.Matches(run.ctx, run.request.ItemID, run.head.PlatformID)
	if err != nil {
		return fmt.Errorf("read approval duplicates: %w", err)
	}
	decision := run.request.Decision
	if len(run.duplicateGames) > 0 && (decision.DuplicatePolicy != "ALLOW_NEW" ||
		!SameApprovalDuplicateIDs(run.duplicateGames, decision.AcknowledgedGameIDs)) {
		return &model.DuplicateConflict{ContentIdentityDigest: digest, Games: run.duplicateGames}
	}
	if len(run.duplicateGames) == 0 && decision.DuplicatePolicy != "" {
		return model.ErrInvalid
	}
	return nil
}

func approvalStaticSnapshot(raw string) (corevalidation.Snapshot, bool) {
	snapshot, err := corevalidation.ParseSnapshot(raw)
	return snapshot, err == nil
}
