package payloadrelease

import (
	"context"
	"fmt"

	model "retrom/internal/model/payloadrelease"
)

func (run *effectRun) consumePayload(ctx context.Context, payload model.EffectPayload, reason model.Reason) error {
	for _, before := range payload.Consumptions {
		if before.Released.Set {
			continue
		}
		if err := run.scope.Write.Consume(
			ctx, model.EffectConsumptionChange{Before: before, Reason: reason, NowMS: run.nowMS},
		); err != nil {
			return fmt.Errorf("release payload consumption: %w", err)
		}
		before.Released = model.WorkTime{Set: true, Value: run.nowMS}
		before.Version++
		run.completed = append(
			run.completed, model.EffectOwner{
				Found: true,
				Owner: model.Owner{Scope: model.Scope{
					Type: model.ScopeUploadConsumption,
					ID:   before.ID,
				}, Version: before.Version},
				Consumption: before,
			},
		)
	}
	return nil
}

func (run *effectRun) purgePayload(ctx context.Context, payload model.EffectPayload) error {
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
			return model.ErrEffectConflict
		}
		cursor = next
	}
}

func (run *effectRun) consumption(ctx context.Context, before model.EffectOwner, reason model.Reason) error {
	payload := model.EffectPayload{Consumptions: []model.EffectConsumption{before.Consumption}}
	if err := run.consumePayload(ctx, payload, reason); err != nil {
		return err
	}
	return run.purgePayload(ctx, payload)
}
