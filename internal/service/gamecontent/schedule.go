package gamecontent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/security/authn"
	"retrom/internal/foundation/cleanup"

	"github.com/google/uuid"
)

func (service *Service) schedule(
	ctx context.Context,
	gameID, uploadID string,
	expectedVersion int64,
	mode, key, digest string,
) (Scheduled, bool, error) {
	if mode == "" {
		mode = contentcapability.ModeStandard
	}
	if mode != contentcapability.ModeStandard && mode != contentcapability.ModeMultiDisc &&
		mode != contentcapability.ModeRPGMakerProject {
		return Scheduled{}, false, ErrInvalid
	}
	now := service.now().UnixMilli()
	principal, _ := authn.PrincipalFromContext(ctx)
	principalID := principal.UserID
	if principalID == "" {
		principalID = "SYSTEM"
	}
	var result Scheduled
	var replayed bool
	err := service.repository.CommitWrite(ctx, func(scope WriteScope) error {
		var err error
		result, replayed, err = loadScheduled(ctx, scope.Replays, principalID, key, digest, now)
		if err != nil || replayed {
			return err
		}
		result, err = service.scheduleFresh(ctx, scope, gameID, uploadID, mode, expectedVersion, now)
		if err != nil {
			return err
		}
		if key != "" {
			return rememberScheduled(ctx, scope.Replays, result, principalID, key, digest, now)
		}
		return nil
	})
	if err != nil {
		return Scheduled{}, false, fmt.Errorf("schedule content replacement: %w", err)
	}
	if !replayed {
		go func() {
			cleanup.Error("run content replacement", service.Run(context.WithoutCancel(ctx), result.JobID, 1))
		}()
	}
	return result, replayed, nil
}

func rememberScheduled(
	ctx context.Context,
	records ReplayRecords,
	result Scheduled,
	principalID, key, digest string,
	now int64,
) error {
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode scheduled replacement: %w", err)
	}
	headers, err := json.Marshal(
		map[string]string{
			"Content-Type": "application/json; charset=utf-8",
			"ETag": fmt.Sprintf(
				`"v%d"`,
				result.Version,
			),
		},
	)
	if err != nil {
		return fmt.Errorf("encode replacement headers: %w", err)
	}
	if err := records.Remember(
		ctx,
		ReplayWrite{
			PrincipalID: principalID,
			Key:         key,
			Digest:      digest,
			Headers:     headers,
			Body:        body,
			Now:         now,
			ExpiresAt: now + int64(
				24*time.Hour/time.Millisecond,
			),
		},
	); err != nil {
		return fmt.Errorf("remember scheduled replacement: %w", err)
	}
	return nil
}

func enqueue(ctx context.Context, jobs JobWriter, snapshot JobSnapshot, now int64) (Scheduled, error) {
	jobID, err := uuid.NewV7()
	if err != nil {
		return Scheduled{}, fmt.Errorf("create replacement job identity: %w", err)
	}
	consumptionID, err := uuid.NewV7()
	if err != nil {
		return Scheduled{}, fmt.Errorf("create replacement consumption identity: %w", err)
	}
	envelope := inputEnvelope{
		SchemaVersion: 1,
		Kind:          "GAME_CONTENT_REPLACE",
		Scope: inputScope{
			"GAME",
			snapshot.GameID,
		},
		ExecutionID: snapshot.ExecutionID,
		Inputs:      snapshot,
	}
	input, err := json.Marshal(envelope)
	if err != nil {
		return Scheduled{}, fmt.Errorf("encode replacement input: %w", err)
	}
	dedupeInput, err := json.Marshal(map[string]any{"executionId": snapshot.ExecutionID, "gameId": snapshot.GameID})
	if err != nil {
		return Scheduled{}, fmt.Errorf("encode replacement identity: %w", err)
	}
	dedupe := sha256.Sum256(append([]byte("retrom-job-dedupe-v1\x00GAME_CONTENT_REPLACE\x00"), dedupeInput...))
	inputDigest := sha256.Sum256(input)
	if err := jobs.Enqueue(ctx, ScheduleWrite{
		JobID: jobID.String(), ConsumptionID: consumptionID.String(), GameID: snapshot.GameID,
		UploadID: snapshot.UploadSessionID, Dedupe: hex.EncodeToString(
			dedupe[:],
		), InputDigest: hex.EncodeToString(
			inputDigest[:],
		), Input: input, Now: now,
	}); err != nil {
		return Scheduled{}, fmt.Errorf("persist replacement job: %w", err)
	}
	return Scheduled{GameID: snapshot.GameID, JobID: jobID.String(), State: "QUEUED", Version: snapshot.GameVersion}, nil
}

func ValidateUpload(upload Upload, mode, platformID string) error {
	if upload.State != "COMPLETE" || upload.FileCount == 0 || upload.Consumptions != 0 {
		return ErrInvalid
	}
	if mode == contentcapability.ModeStandard && platformID != "dos" && upload.FileCount != 1 {
		return ErrInvalid
	}
	if (mode == contentcapability.ModeMultiDisc || mode == contentcapability.ModeRPGMakerProject) &&
		upload.SourceType != "DIRECTORY" {
		return ErrInvalid
	}
	return nil
}

func loadScheduled(
	ctx context.Context,
	records ReplayRecords,
	principalID, key, digest string,
	now int64,
) (Scheduled, bool, error) {
	if key == "" {
		return Scheduled{}, false, nil
	}
	stored, found, err := records.Load(ctx, principalID, key, now)
	if err != nil {
		return Scheduled{}, false, fmt.Errorf("read replacement replay: %w", err)
	}
	if !found {
		return Scheduled{}, false, nil
	}
	if stored.Digest != digest {
		return Scheduled{}, false, ErrIdempotencyKeyReused
	}
	var result Scheduled
	if err := json.Unmarshal(stored.Body, &result); err != nil {
		return Scheduled{}, false, fmt.Errorf("%w: decode replay: %w", ErrInvalid, err)
	}
	return result, true, nil
}
