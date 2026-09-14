package libraryimport

import (
	"context"
	"fmt"
	"time"

	"retrom/internal/capability/security/authn"
	application "retrom/internal/model/libraryimport"
	"retrom/internal/service/tagging"
)

type ReviewDraftsOptions struct {
	Validation ReviewDraftValidationPort
	Now        func() time.Time
}

// ReviewDrafts coordinates review draft commands. It reads a storage-neutral
// snapshot, makes the validation/tag decisions in the application layer, and
// hands a value-only plan to the repository for one atomic commit.
type ReviewDrafts struct {
	repository ReviewDraftPatchRepository
	validation ReviewDraftValidationPort
	now        func() time.Time
}

func NewReviewDrafts(repository ReviewDraftPatchRepository, options ...ReviewDraftsOptions) *ReviewDrafts {
	configured := ReviewDraftsOptions{}
	if len(options) > 0 {
		configured = options[0]
	}
	if configured.Now == nil {
		configured.Now = time.Now
	}
	return &ReviewDrafts{repository: repository, validation: configured.Validation, now: configured.Now}
}

func (service *ReviewDrafts) Patch(
	ctx context.Context, itemID string, expectedVersion int64, patch DraftPatch,
) (DraftResult, error) {
	if err := ValidateDraftPatch(patch); err != nil {
		return DraftResult{}, err
	}
	if service == nil || service.repository == nil {
		return DraftResult{}, ErrInvalid
	}
	now := service.now()
	actor := authn.ActorFromContext(ctx, "release-setup")
	snapshot, err := service.repository.LoadPatchSnapshot(ctx, application.ReviewDraftPatchQuery{
		ItemID: itemID, ExpectedVersion: expectedVersion, TagIDs: patch.TagIDs,
	})
	if err != nil {
		return DraftResult{}, fmt.Errorf("load review draft patch snapshot: %w", err)
	}
	plan, err := service.buildPatchPlan(ctx, itemID, expectedVersion, patch, snapshot, actor, now)
	if err != nil {
		return DraftResult{}, err
	}
	result, err := service.repository.CommitPatch(ctx, plan)
	if err != nil {
		return DraftResult{}, fmt.Errorf("patch review draft: %w", err)
	}
	return result, nil
}

func (service *ReviewDrafts) buildPatchPlan(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	patch DraftPatch,
	snapshot application.ReviewDraftPatchSnapshot,
	actor authn.Actor,
	now time.Time,
) (application.ReviewDraftWritePlan, error) {
	metadata, err := application.MergeMetadata(snapshot.Metadata, patch.Metadata, now)
	if err != nil {
		return application.ReviewDraftWritePlan{}, fmt.Errorf("merge review draft metadata: %w", err)
	}
	actorUserID := actorString(actor.UserID)
	desiredTags, err := tagging.ValidateActiveReferenceFacts(patch.TagIDs, snapshot.ActiveTags)
	if err != nil {
		return application.ReviewDraftWritePlan{}, fmt.Errorf("validate review draft tags: %w", err)
	}
	tagPlan, err := tagging.BuildReplacementPlan(
		tagging.Owner{Kind: tagging.OwnerReviewDraft, ID: snapshot.DraftID},
		snapshot.BeforeTags, desiredTags, actorUserID, now.UnixMilli(),
	)
	if err != nil {
		return application.ReviewDraftWritePlan{}, fmt.Errorf("plan review draft tags: %w", err)
	}
	targetID, dosEntry := patchedTargetAndDOS(snapshot, patch)
	validationPlan, err := service.resolveValidationPlan(ctx, itemID, targetID, dosEntry, patch)
	if err != nil {
		return application.ReviewDraftWritePlan{}, fmt.Errorf("resolve review draft validation: %w", err)
	}
	plan := application.ReviewDraftWritePlan{
		ItemID: itemID, DraftID: snapshot.DraftID, ExpectedVersion: expectedVersion,
		ExpectedTargetID: snapshot.TargetID, ExpectedValidationID: snapshot.ValidationID,
		ExpectedEffectiveSnapshotID: snapshot.EffectiveSnapshotID,
		ExpectedDOSEntry:            copyString(snapshot.DOSEntry), ExpectedIsRPG: snapshot.IsRPG,
		TargetID: targetID, ValidationID: validationPlan.SelectedValidationID,
		CandidateID: patchString(snapshot.CandidateID), CoverID: patchString(snapshot.CoverID),
		UploadedCoverID: patchString(snapshot.UploadedCoverID), BackgroundID: patchString(snapshot.BackgroundID),
		DOSEntry: dosEntry, Metadata: metadata,
		SearchParts:        application.SearchParts(itemID, snapshot.SourcePaths, metadata),
		ScreenshotAssetIDs: append([]string(nil), snapshot.ScreenshotAssetIDs...),
		AssetsChanged:      patch.SelectedAssets != nil, Tags: tagPlan,
		ActorKind: actor.Kind, ActorUserID: actorUserIDPtr(actor.UserID), ActorLabel: actorStringPtr(actor.Label),
		RPGSelfContainedOverride: patch.RPGSelfContainedOverride,
		RPGDependencyDigest:      validationPlan.RPGDependencyDigest,
		ValidationCreate:         validationPlan.Create, ValidationCopy: validationPlan.Copy, NowMS: now.UnixMilli(),
	}
	applyCandidatePatch(&plan, patch)
	applyAssetPatch(&plan, patch)
	return plan, nil
}

func patchedTargetAndDOS(
	snapshot application.ReviewDraftPatchSnapshot, patch DraftPatch,
) (string, *string) {
	targetID := snapshot.TargetID
	if patch.TargetPlatformInstanceID != nil {
		targetID = *patch.TargetPlatformInstanceID
	}
	dosEntry := copyString(snapshot.DOSEntry)
	if present, value := patch.DefaultDOSEntry.Optional(); present {
		dosEntry = copyString(value)
	}
	return targetID, dosEntry
}

func (service *ReviewDrafts) resolveValidationPlan(
	ctx context.Context,
	itemID string,
	targetID string,
	dosEntry *string,
	patch DraftPatch,
) (application.ReviewValidationPlan, error) {
	if service.validation == nil && (patch.ScummVMCandidateID != nil || patch.SelectedValidationID == nil) {
		return application.ReviewValidationPlan{}, ErrInvalid
	}
	plan := application.ReviewValidationPlan{}
	var err error
	if patch.ScummVMCandidateID != nil {
		plan, err = service.validation.SelectScummVM(ctx, ReviewDraftScummVMRequest{
			ItemID: itemID, TargetPlatformInstanceID: targetID, DefaultDOSEntry: dosEntry,
			CandidateID: *patch.ScummVMCandidateID,
		})
	} else if patch.SelectedValidationID == nil {
		plan, err = service.validation.Resolve(ctx, ReviewDraftValidationRequest{
			ItemID: itemID, TargetPlatformInstanceID: targetID, DefaultDOSEntry: dosEntry,
			RPGSelfContainedOverride: patch.RPGSelfContainedOverride,
		})
	}
	if err != nil {
		return application.ReviewValidationPlan{}, fmt.Errorf("review validation: %w", err)
	}
	if patch.SelectedValidationID != nil {
		if *patch.SelectedValidationID == "" {
			return application.ReviewValidationPlan{}, ErrInvalid
		}
		plan.SelectedValidationID = *patch.SelectedValidationID
		plan.Create = nil
		plan.Copy = nil
		plan.RPGDependencyDigest = ""
	}
	return plan, nil
}

func applyCandidatePatch(plan *application.ReviewDraftWritePlan, patch DraftPatch) {
	if present, value := patch.SelectedCandidateID.Optional(); present {
		plan.CandidateID = copyString(value)
	}
}

func applyAssetPatch(plan *application.ReviewDraftWritePlan, patch DraftPatch) {
	if patch.SelectedAssets == nil {
		return
	}
	plan.CoverID = copyString(patch.SelectedAssets.CoverCandidateAssetID)
	plan.UploadedCoverID = copyString(patch.SelectedAssets.CoverUploadedAssetID)
	plan.BackgroundID = copyString(patch.SelectedAssets.BackgroundCandidateAssetID)
	plan.ScreenshotAssetIDs = append([]string(nil), patch.SelectedAssets.ScreenshotCandidateAssetIDs...)
}

func actorString(value any) string {
	text, _ := value.(string)
	return text
}

func actorUserIDPtr(value any) *string {
	text := actorString(value)
	if text == "" {
		return nil
	}
	return &text
}

func actorStringPtr(value any) *string {
	text, ok := value.(string)
	if !ok || text == "" {
		return nil
	}
	return &text
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func patchString(value *string) *string { return copyString(value) }
