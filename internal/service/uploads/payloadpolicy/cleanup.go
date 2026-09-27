package payloadpolicy

import (
	"context"
	"fmt"

	jobs "retrom/internal/service/cleanupjobs"
)

func ConsumptionCleanup(ctx context.Context, scope jobs.EffectScope, unit jobs.Execution, now int64) (bool, error) {
	before, err := scope.Read.Owner(ctx, unit.Work.Scope)
	if err != nil {
		return false, fmt.Errorf("read upload consumption: %w", err)
	}
	if !before.Found || before.Owner.Scope != unit.Work.Scope {
		return false, jobs.ErrScopeInvalid
	}
	if before.Owner.Version != unit.Input.Inputs.ScopeVersion && !before.Consumption.Released.Set {
		return false, jobs.ErrEffectConflict
	}
	payload := jobs.EffectPayload{Consumptions: []jobs.EffectConsumption{before.Consumption}}
	if err := ConsumePayload(ctx, scope, payload, unit.Input.Inputs.Reason, now); err != nil {
		return false, fmt.Errorf("release uploads payload: %w", err)
	}
	return PurgePayload(ctx, scope, payload, now)
}

func ConsumePayload(ctx context.Context, scope jobs.EffectScope, payload jobs.EffectPayload,
	reason jobs.Reason, now int64,
) error {
	for _, before := range payload.Consumptions {
		if before.Released.Set {
			continue
		}
		if err := scope.Write.Consume(ctx, jobs.EffectConsumptionChange{
			Before: before, Reason: reason, NowMS: now,
		}); err != nil {
			return fmt.Errorf("release upload consumption: %w", err)
		}
		before.Released = jobs.WorkTime{Set: true, Value: now}
		before.Version++
		expected := jobs.EffectOwner{
			Found: true,
			Owner: jobs.Owner{
				Scope:   jobs.Scope{Type: jobs.ScopeUploadConsumption, ID: before.ID},
				Version: before.Version,
			},
			Consumption: before,
		}
		current, err := scope.Read.Owner(ctx, expected.Owner.Scope)
		if err != nil {
			return fmt.Errorf("confirm upload consumption: %w", err)
		}
		if current != expected {
			return jobs.ErrEffectConflict
		}
	}
	return nil
}

func PurgePayload(ctx context.Context, scope jobs.EffectScope, payload jobs.EffectPayload, now int64) (bool, error) {
	seen := make(map[string]bool)
	more := false
	for _, consumption := range payload.Consumptions {
		if seen[consumption.SessionID] {
			continue
		}
		seen[consumption.SessionID] = true
		files, err := scope.Uploads.Candidates(ctx, consumption.SessionID, "", 200)
		if err != nil {
			return false, fmt.Errorf("read upload release candidates: %w", err)
		}
		for _, file := range files {
			if !CanPurge(file.State, file.SessionState, file.ID, file.FileRecord, file.ActiveConsumptions) {
				continue
			}
			if err := scope.Uploads.Purge(ctx, file, now); err != nil {
				return false, fmt.Errorf("purge released upload: %w", err)
			}
		}
		more = more || len(files) == 200
	}
	return more, nil
}
