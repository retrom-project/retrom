package saves

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"time"

	"retrom/internal/adapter/files/blobstore"
	saves "retrom/internal/model/saves"
)

func executeManualCheckpoint(ctx context.Context, scope saves.WriteScope, cmd saves.ManualCheckpointCommand) (saves.ManualResult, bool, error) {
	now := cmd.IdempotencyKey.AtMS

	previous, found, err := scope.Idempotency.Replay(ctx, cmd.IdempotencyKey)
	if err != nil {
		return saves.ManualResult{}, false, fmt.Errorf("read checkpoint replay: %w", err)
	}
	if found {
		if subtle.ConstantTimeCompare([]byte(previous.Digest), []byte(cmd.Digest)) != 1 {
			return saves.ManualResult{}, false, saves.ErrSequenceReused
		}
		var result saves.ManualResult
		if err := json.Unmarshal(previous.Body, &result); err != nil {
			return saves.ManualResult{}, false, fmt.Errorf("decode checkpoint replay: %w", err)
		}
		return result, true, nil
	}

	current, err := scope.Launches.LoadLaunch(ctx, cmd.LaunchID)
	if err != nil {
		return saves.ManualResult{}, false, fmt.Errorf("verify launch: %w", err)
	}
	if current.State != "ACTIVE" || current.HardExpiresAtMS < now {
		return saves.ManualResult{}, false, saves.ErrCredential
	}
	if cmd.Payload.Size > 16*1024*1024 {
		return saves.ManualResult{}, false, saves.ErrTooLarge
	}

	payload := blobstore.Metadata{SHA256: cmd.Payload.SHA256, Size: cmd.Payload.Size}
	payloadID, err := scope.Blobs.Ensure(ctx, payload, cmd.Payload.MediaType, now)
	if err != nil {
		return saves.ManualResult{}, false, fmt.Errorf("register checkpoint payload: %w", err)
	}

	var result saves.ManualResult
	if cmd.Launch.Purpose == "PRODUCT" {
		result, err = persistProduct(ctx, scope, cmd, payloadID, now)
	} else {
		result = saves.ManualResult{
			ResourceKind:     "REVIEW_PREVIEW_CHECKPOINT",
			PreviewID:        cmd.LaunchID,
			CheckpointFormat: cmd.Launch.Checkpoint.WriteFormat,
			CreatedAtMS:      now,
		}
		err = scope.Checkpoints.ReplacePreview(ctx, saves.PreviewWrite{
			PreviewID: cmd.LaunchID, PayloadID: payloadID,
			Format: cmd.Launch.Checkpoint.WriteFormat, AtMS: now,
		})
	}
	if err != nil {
		return saves.ManualResult{}, false, fmt.Errorf("persist checkpoint: %w", err)
	}

	body, err := json.Marshal(result)
	if err != nil {
		return saves.ManualResult{}, false, fmt.Errorf("encode checkpoint response: %w", err)
	}
	if err := scope.Idempotency.Remember(ctx, saves.ReplayWrite{
		ReplayKey:   cmd.IdempotencyKey,
		Replay:      saves.Replay{Digest: cmd.Digest, Body: body},
		ExpiresAtMS: now + int64(24*time.Hour/time.Millisecond),
	}); err != nil {
		return saves.ManualResult{}, false, fmt.Errorf("remember checkpoint response: %w", err)
	}
	return result, false, nil
}

func persistProduct(ctx context.Context, scope saves.WriteScope, cmd saves.ManualCheckpointCommand, payloadID string, now int64) (saves.ManualResult, error) {
	binding, found, err := scope.GameSaves.Binding(ctx, cmd.LaunchID)
	if err != nil {
		return saves.ManualResult{}, fmt.Errorf("load game save binding: %w", err)
	}
	if !found {
		return insertProduct(ctx, scope, cmd, payloadID, now)
	}
	if cmd.Screenshot == nil {
		return saves.ManualResult{}, saves.ErrCheckpointInvalid
	}
	var result saves.ManualResult
	var dataVersion int64
	if binding.ID == nil {
		if binding.ExpectedVersion != 0 {
			return saves.ManualResult{}, saves.ErrSyncConflict
		}
		result, err = insertProduct(ctx, scope, cmd, payloadID, now)
		dataVersion = 1
		if err == nil {
			err = scope.GameSaves.MarkSynced(ctx, result.SaveStateID, cmd.LaunchID, now)
		}
	} else {
		result, dataVersion, err = updateProduct(ctx, scope, cmd, payloadID, binding, now)
	}
	if err != nil {
		return saves.ManualResult{}, fmt.Errorf("publish game save: %w", err)
	}
	if err := scope.GameSaves.Bind(ctx, cmd.LaunchID, result.SaveStateID, dataVersion); err != nil {
		return saves.ManualResult{}, fmt.Errorf("advance game save binding: %w", err)
	}
	return result, nil
}

func insertProduct(ctx context.Context, scope saves.WriteScope, cmd saves.ManualCheckpointCommand, payloadID string, now int64) (saves.ManualResult, error) {
	var screenshotID *string
	if cmd.Screenshot != nil {
		meta := blobstore.Metadata{SHA256: cmd.Screenshot.SHA256, Size: cmd.Screenshot.Size}
		value, err := scope.Blobs.Ensure(ctx, meta, cmd.ScreenshotMediaType, now)
		if err != nil {
			return saves.ManualResult{}, fmt.Errorf("register save screenshot: %w", err)
		}
		screenshotID = &value
	}
	duration, err := scope.Checkpoints.Duration(ctx, cmd.LaunchID)
	if err != nil {
		return saves.ManualResult{}, fmt.Errorf("read save duration: %w", err)
	}
	result := saves.ManualResult{
		ResourceKind:     "SAVE_STATE",
		SaveStateID:      cmd.SaveStateID,
		CheckpointFormat: cmd.Launch.Checkpoint.WriteFormat,
		CreatedAtMS:      now,
		Name:             cmd.MetadataName,
		DiscIndex:        cmd.MetadataDiscIndex,
		Version:          1,
		ActiveDurationMS: duration.ActiveMS,
	}
	result.ScreenshotURL = screenshotURL(result.SaveStateID, screenshotID)
	payload := blobstore.Metadata{SHA256: cmd.Payload.SHA256, Size: cmd.Payload.Size}
	if err := scope.Checkpoints.CreateSave(ctx, saves.SaveCreation{
		LaunchID: cmd.LaunchID, ProfileID: cmd.Launch.ProfileID, GameID: cmd.Launch.GameID, PayloadID: payloadID,
		DOSEntry: cmd.Launch.DOSEntry, ScreenshotID: screenshotID, Payload: payload, Result: result,
	}); err != nil {
		return saves.ManualResult{}, fmt.Errorf("create save: %w", err)
	}
	return result, nil
}

func updateProduct(ctx context.Context, scope saves.WriteScope, cmd saves.ManualCheckpointCommand, payloadID string, binding saves.GameSaveBinding, now int64) (saves.ManualResult, int64, error) {
	saved, found, err := scope.GameSaves.Saved(ctx, *binding.ID)
	if err != nil {
		return saves.ManualResult{}, 0, fmt.Errorf("load saved slot: %w", err)
	}
	if !found || saved.DataVersion != binding.ExpectedVersion || saved.ProfileID != cmd.Launch.ProfileID ||
		saved.GameID != cmd.Launch.GameID || saved.Format != cmd.Launch.Checkpoint.WriteFormat || saved.DeletedAtMS != nil {
		return saves.ManualResult{}, 0, saves.ErrSyncConflict
	}
	result := saved.Result
	result.ResourceKind = "SAVE_STATE"
	result.CheckpointFormat = saved.Format
	result.ScreenshotURL = screenshotURL(result.SaveStateID, saved.ScreenshotID)
	if saved.Digest == cmd.Payload.SHA256 {
		return result, saved.DataVersion, nil
	}
	imageID, err := scope.Blobs.Ensure(ctx, blobstore.Metadata{SHA256: cmd.Screenshot.SHA256, Size: cmd.Screenshot.Size}, cmd.ScreenshotMediaType, now)
	if err != nil {
		return saves.ManualResult{}, 0, fmt.Errorf("register game save image: %w", err)
	}
	duration, err := scope.Checkpoints.Duration(ctx, cmd.LaunchID)
	if err != nil {
		return saves.ManualResult{}, 0, fmt.Errorf("read game save duration: %w", err)
	}
	result.ActiveDurationMS = duration.InitialMS + duration.ActiveMS
	payload := blobstore.Metadata{SHA256: cmd.Payload.SHA256, Size: cmd.Payload.Size}
	if err := scope.GameSaves.UpdateSave(ctx, saves.SaveUpdate{
		SaveID: result.SaveStateID, LaunchID: cmd.LaunchID, PayloadID: payloadID, ScreenshotID: imageID,
		Payload: payload, ExpectedDataVersion: binding.ExpectedVersion, AtMS: now,
		ActiveDurationMS: result.ActiveDurationMS,
	}); err != nil {
		return saves.ManualResult{}, 0, fmt.Errorf("update saved slot: %w", err)
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
