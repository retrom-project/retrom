package gamecontent

import (
	"context"
	"fmt"
	"math"

	"retrom/internal/service/payloadrelease"
)

func RetireInScope(
	ctx context.Context, scope RetirementScope, gameID, selectedVariantID string, now int64,
) (RetirementImpact, error) {
	blobs, err := scope.Read.Blobs(ctx, gameID)
	if err != nil {
		return RetirementImpact{}, fmt.Errorf("read replaced content blobs: %w", err)
	}
	owners, err := scope.Read.Owners(ctx, gameID)
	if err != nil {
		return RetirementImpact{}, fmt.Errorf("read replaced content runtime: %w", err)
	}
	for _, before := range owners {
		change, needed, err := retirementChange(before, gameID, selectedVariantID, now)
		if err != nil {
			return RetirementImpact{}, err
		}
		if !needed {
			continue
		}
		if err := scope.Write.Change(ctx, change); err != nil {
			return RetirementImpact{}, fmt.Errorf("retire replaced content runtime: %w", err)
		}
	}
	impact := RetirementImpact{CandidateBlobIDs: blobs}
	for _, kind := range []RetirementReferenceKind{
		RetirementSave, RetirementLaunchExternal, RetirementLaunchContent,
		RetirementVariantFile, RetirementVariantDependency,
	} {
		count, err := retireReferences(ctx, scope, gameID, kind)
		if err != nil {
			return RetirementImpact{}, err
		}
		if kind == RetirementSave {
			impact.SaveStateCount = count
		}
	}
	return impact, nil
}

func retirementChange(before RetirementOwner, gameID, selected string, now int64) (RetirementChange, bool, error) {
	change := RetirementChange{
		Before: before, GameID: gameID, State: before.State,
		Reason: "GAME_CONTENT_REPLACED", Now: now,
	}
	state, needed, err := retirementState(before, selected)
	if err != nil {
		return RetirementChange{}, false, err
	}
	change.State = state
	if needed && (before.ID == "" || before.Version < 1 || before.Version == math.MaxInt64) {
		return RetirementChange{}, false, ErrInvalid
	}
	change.FinishedAt = before.FinishedAt
	if needed && before.Kind != RetirementVariant &&
		(before.Kind != RetirementLaunch || change.State != before.State) && change.FinishedAt == nil {
		change.FinishedAt = &change.Now
	}
	return change, needed, nil
}

func retireReferences(
	ctx context.Context, scope RetirementScope, gameID string, kind RetirementReferenceKind,
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
			return 0, ErrInvalid
		}
		if err := scope.Write.Remove(ctx, gameID, kind, references); err != nil {
			return 0, fmt.Errorf("remove replaced content references: %w", err)
		}
		count += int64(len(references))
	}
}

func releaseReplacementUpload(ctx context.Context, scope RetirementScope, jobID string, now int64) error {
	id, err := scope.Read.Consumption(ctx, jobID)
	if err != nil {
		return fmt.Errorf("read replacement upload consumption: %w", err)
	}
	if _, err := payloadrelease.NewScheduler(nil).Consumption(ctx, scope.Payload, id, now); err != nil {
		return fmt.Errorf("release replacement upload: %w", err)
	}
	return nil
}

func retirementState(before RetirementOwner, selected string) (string, bool, error) {
	state := before.State
	needed := false
	switch before.Kind {
	case RetirementLaunch:
		if before.State == "CREATED" || before.State == "ACTIVE" {
			state = "REVOKED"
			needed = true
		}
		needed = needed || before.SaveID != nil
	case RetirementPlay:
		needed = before.State == "ACTIVE"
		state = "ABANDONED"
	case RetirementVariant:
		needed = before.ID != selected
		state = "BLOCKED"
	default:
		return "", false, ErrInvalid
	}
	return state, needed, nil
}
