package gamecontent

import (
	"context"
	"fmt"
	"math"

	model "retrom/internal/model/gamecontent"

	"retrom/internal/service/payloadrelease"
)

func RetireInScope(
	ctx context.Context, scope model.RetirementScope, gameID, selectedVariantID string, now int64,
) (model.RetirementImpact, error) {
	blobs, err := scope.Read.Blobs(ctx, gameID)
	if err != nil {
		return model.RetirementImpact{}, fmt.Errorf("read replaced content blobs: %w", err)
	}
	owners, err := scope.Read.Owners(ctx, gameID)
	if err != nil {
		return model.RetirementImpact{}, fmt.Errorf("read replaced content runtime: %w", err)
	}
	for _, before := range owners {
		change, needed, err := retirementChange(before, gameID, selectedVariantID, now)
		if err != nil {
			return model.RetirementImpact{}, err
		}
		if !needed {
			continue
		}
		if err := scope.Write.Change(ctx, change); err != nil {
			return model.RetirementImpact{}, fmt.Errorf("retire replaced content runtime: %w", err)
		}
	}
	impact := model.RetirementImpact{CandidateBlobIDs: blobs}
	for _, kind := range []model.RetirementReferenceKind{
		model.RetirementSave, model.RetirementLaunchExternal, model.RetirementLaunchContent,
		model.RetirementVariantFile, model.RetirementVariantDependency,
	} {
		count, err := retireReferences(ctx, scope, gameID, kind)
		if err != nil {
			return model.RetirementImpact{}, err
		}
		if kind == model.RetirementSave {
			impact.SaveStateCount = count
		}
	}
	return impact, nil
}

func retirementChange(
	before model.RetirementOwner,
	gameID, selected string,
	now int64,
) (model.RetirementChange, bool, error) {
	change := model.RetirementChange{
		Before: before, GameID: gameID, State: before.State,
		Reason: "GAME_CONTENT_REPLACED", Now: now,
	}
	state, needed, err := retirementState(before, selected)
	if err != nil {
		return model.RetirementChange{}, false, err
	}
	change.State = state
	if needed && (before.ID == "" || before.Version < 1 || before.Version == math.MaxInt64) {
		return model.RetirementChange{}, false, model.ErrInvalid
	}
	change.FinishedAt = before.FinishedAt
	if needed && before.Kind != model.RetirementVariant &&
		(before.Kind != model.RetirementLaunch || change.State != before.State) && change.FinishedAt == nil {
		change.FinishedAt = &change.Now
	}
	return change, needed, nil
}

func retireReferences(
	ctx context.Context, scope model.RetirementScope, gameID string, kind model.RetirementReferenceKind,
) (int64, error) {
	var count int64
	for {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("stop content retirement: %w", err)
		}
		references, err := scope.Read.References(ctx, gameID, kind, 200)
		if err != nil {
			return 0, fmt.Errorf("read replaced content references: %w", err)
		}
		if len(references) == 0 {
			return count, nil
		}
		if len(references) > 200 || count > math.MaxInt64-int64(len(references)) {
			return 0, model.ErrInvalid
		}
		if err := scope.Write.Remove(ctx, gameID, kind, references); err != nil {
			return 0, fmt.Errorf("remove replaced content references: %w", err)
		}
		count += int64(len(references))
	}
}

func releaseReplacementUpload(ctx context.Context, scope model.RetirementScope, jobID string, now int64) error {
	id, err := scope.Read.Consumption(ctx, jobID)
	if err != nil {
		return fmt.Errorf("read replacement upload consumption: %w", err)
	}
	if _, err := payloadrelease.NewScheduler(nil).Consumption(ctx, scope.Payload, id, now); err != nil {
		return fmt.Errorf("release replacement upload: %w", err)
	}
	return nil
}

func retirementState(before model.RetirementOwner, selected string) (string, bool, error) {
	state := before.State
	needed := false
	switch before.Kind {
	case model.RetirementLaunch:
		if before.State == "CREATED" || before.State == "ACTIVE" {
			state = "REVOKED"
			needed = true
		}
		needed = needed || before.SaveID != nil
	case model.RetirementPlay:
		needed = before.State == "ACTIVE"
		state = "ABANDONED"
	case model.RetirementNetplay:
		needed = before.State != "FINISHED" && before.State != "FAILED"
		state = "FAILED"
	case model.RetirementRoom:
		needed = before.State == "WAITING" || before.State == "STARTING" || before.State == "RUNNING"
		state = "ENDED"
	case model.RetirementVariant:
		needed = before.ID != selected
		state = "BLOCKED"
	default:
		return "", false, model.ErrInvalid
	}
	return state, needed, nil
}
