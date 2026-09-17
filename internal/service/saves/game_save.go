package saves

import (
	"context"
	"fmt"

	model "retrom/internal/model/saves"

	"github.com/google/uuid"
)

func (service *Service) persistProductCheckpoint(ctx context.Context, scope model.WriteScope, id string,
	launch model.Launch, parsed parsedManual, payloadID string, now int64,
) (model.ManualResult, error) {
	binding, found, err := scope.GameSaves.Binding(ctx, id)
	if err != nil {
		return model.ManualResult{}, fmt.Errorf("load game save binding: %w", err)
	}
	if !found {
		return service.insertProductSave(ctx, scope, id, launch, parsed, payloadID, now)
	}
	if parsed.screenshot == nil {
		return model.ManualResult{}, model.ErrCheckpointInvalid
	}
	var result model.ManualResult
	var dataVersion int64
	if binding.ID == nil {
		if binding.ExpectedVersion != 0 {
			return model.ManualResult{}, model.ErrSyncConflict
		}
		result, err = service.insertProductSave(ctx, scope, id, launch, parsed, payloadID, now)
		dataVersion = 1
		if err == nil {
			err = scope.GameSaves.MarkSynced(ctx, result.SaveStateID, id, now)
		}
	} else {
		result, dataVersion, err = service.updateGameSave(ctx, scope, id, launch, parsed, payloadID, binding, now)
	}
	if err != nil {
		return model.ManualResult{}, fmt.Errorf("publish game save: %w", err)
	}
	if err := scope.GameSaves.Bind(ctx, id, result.SaveStateID, dataVersion); err != nil {
		return model.ManualResult{}, fmt.Errorf("advance game save binding: %w", err)
	}
	return result, nil
}

func (service *Service) insertProductSave(ctx context.Context, scope model.WriteScope, id string,
	launch model.Launch, parsed parsedManual, payloadID string, now int64,
) (model.ManualResult, error) {
	var screenshotID *string
	if parsed.screenshot != nil {
		value, err := scope.Blobs.Ensure(ctx, *parsed.screenshot, parsed.screenshotMediaType, now)
		if err != nil {
			return model.ManualResult{}, fmt.Errorf("register save screenshot: %w", err)
		}
		screenshotID = &value
	}
	duration, err := scope.Checkpoints.Duration(ctx, id)
	if err != nil {
		return model.ManualResult{}, fmt.Errorf("read save duration: %w", err)
	}
	generated, err := uuid.NewV7()
	if err != nil {
		return model.ManualResult{}, fmt.Errorf("generate save identifier: %w", err)
	}
	result := model.ManualResult{
		ResourceKind: "SAVE_STATE", SaveStateID: generated.String(),
		CheckpointFormat: launch.Checkpoint.WriteFormat, CreatedAtMS: now, Name: parsed.metadata.Name,
		DiscIndex: parsed.metadata.DiscIndex, Version: 1, ActiveDurationMS: duration.ActiveMS,
	}
	result.ScreenshotURL = screenshotURL(result.SaveStateID, screenshotID)
	if err := scope.Checkpoints.CreateSave(ctx, model.SaveCreation{
		LaunchID: id, ProfileID: launch.ProfileID, GameID: launch.GameID, PayloadID: payloadID,
		DOSEntry: launch.DOSEntry, ScreenshotID: screenshotID, Payload: parsed.payload, Result: result,
	}); err != nil {
		return model.ManualResult{}, fmt.Errorf("create save: %w", err)
	}
	return result, nil
}

func (service *Service) updateGameSave(ctx context.Context, scope model.WriteScope, id string,
	launch model.Launch, parsed parsedManual, payloadID string, binding model.GameSaveBinding, now int64,
) (model.ManualResult, int64, error) {
	saved, found, err := scope.GameSaves.Saved(ctx, *binding.ID)
	if err != nil {
		return model.ManualResult{}, 0, fmt.Errorf("load saved slot: %w", err)
	}
	if !found || saved.DataVersion != binding.ExpectedVersion || saved.ProfileID != launch.ProfileID ||
		saved.GameID != launch.GameID || saved.Format != launch.Checkpoint.WriteFormat || saved.DeletedAtMS != nil {
		return model.ManualResult{}, 0, model.ErrSyncConflict
	}
	result := saved.Result
	result.ResourceKind = "SAVE_STATE"
	result.CheckpointFormat = saved.Format
	result.ScreenshotURL = screenshotURL(result.SaveStateID, saved.ScreenshotID)
	if saved.Digest == parsed.payload.SHA256 {
		return result, saved.DataVersion, nil
	}
	imageID, err := scope.Blobs.Ensure(ctx, *parsed.screenshot, parsed.screenshotMediaType, now)
	if err != nil {
		return model.ManualResult{}, 0, fmt.Errorf("register game save image: %w", err)
	}
	duration, err := scope.Checkpoints.Duration(ctx, id)
	if err != nil {
		return model.ManualResult{}, 0, fmt.Errorf("read game save duration: %w", err)
	}
	result.ActiveDurationMS = duration.InitialMS + duration.ActiveMS
	if err := scope.GameSaves.UpdateSave(ctx, model.SaveUpdate{
		SaveID: result.SaveStateID, LaunchID: id, PayloadID: payloadID, ScreenshotID: imageID,
		Payload: parsed.payload, ExpectedDataVersion: binding.ExpectedVersion, AtMS: now,
		ActiveDurationMS: result.ActiveDurationMS,
	}); err != nil {
		return model.ManualResult{}, 0, fmt.Errorf("update saved slot: %w", err)
	}
	result.Version++
	result.ScreenshotURL = screenshotURL(result.SaveStateID, &imageID)
	return result, saved.DataVersion + 1, nil
}

func screenshotURL(id string, imageID *string) *string {
	if imageID == nil {
		return nil
	}
	value := "/content/save-states/" + id + "/screenshot"
	return &value
}
