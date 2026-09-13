package payloadrelease

import (
	"context"
	"fmt"
)

func (run *effectRun) consumePayload(ctx context.Context, payload EffectPayload, reason Reason) error {
	for _, before := range payload.Consumptions {
		if before.Released.Set {
			continue
		}
		if err := run.scope.Write.Consume(
			ctx,
			EffectConsumptionChange{Before: before, Reason: reason, NowMS: run.nowMS},
		); err != nil {
			return fmt.Errorf("release payload consumption: %w", err)
		}
		before.Released = WorkTime{Set: true, Value: run.nowMS}
		before.Version++
		run.completed = append(
			run.completed,
			EffectOwner{
				Found:       true,
				Owner:       Owner{Scope: Scope{Type: ScopeUploadConsumption, ID: before.ID}, Version: before.Version},
				Consumption: before,
			},
		)
	}
	return nil
}

func (run *effectRun) purgePayload(ctx context.Context, payload EffectPayload) error {
	run.blobs = append(run.blobs, payload.BlobIDs...)
	seen := make(map[string]bool)
	for _, consumption := range payload.Consumptions {
		if seen[consumption.SessionID] {
			continue
		}
		seen[consumption.SessionID] = true
		if err := run.purgeSession(ctx, consumption.SessionID); err != nil {
			return err
		}
	}
	return nil
}

func (run *effectRun) purgeSession(ctx context.Context, sessionID string) error {
	cursor := ""
	for {
		files, err := run.scope.Uploads.Candidates(ctx, sessionID, cursor, 200)
		if err != nil {
			return fmt.Errorf("read upload release candidates: %w", err)
		}
		for _, file := range files {
			if !eligibleEffectUpload(file) {
				continue
			}
			if err := run.scope.Uploads.Purge(ctx, file, run.nowMS); err != nil {
				return fmt.Errorf("purge released upload: %w", err)
			}
			run.blobs = append(run.blobs, file.BlobID)
		}
		if len(files) < 200 {
			return nil
		}
		next := files[len(files)-1].ID
		if next <= cursor {
			return ErrEffectConflict
		}
		cursor = next
	}
}

func (run *effectRun) consumption(ctx context.Context, before EffectOwner, reason Reason) error {
	payload := EffectPayload{Consumptions: []EffectConsumption{before.Consumption}}
	if err := run.consumePayload(ctx, payload, reason); err != nil {
		return err
	}
	return run.purgePayload(ctx, payload)
}
