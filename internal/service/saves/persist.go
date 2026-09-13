package saves

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

func (service *Service) CreateManual(ctx context.Context, id, capability, key string,
	request ManualUpload,
) (ManualResult, bool, error) {
	launch, err := service.launch(ctx, id, capability)
	if err != nil {
		return ManualResult{}, false, err
	}
	return service.createManualForLaunch(ctx, id, key, request, launch)
}

func (service *Service) createManualForLaunch(ctx context.Context, id, key string,
	request ManualUpload, launch Launch,
) (ManualResult, bool, error) {
	parsed, err := service.parseManual(request, launch)
	if err != nil {
		return ManualResult{}, false, err
	}
	metadata, _ := json.Marshal(parsed.metadata)
	screenshot := ""
	if parsed.screenshot != nil {
		screenshot = parsed.screenshot.SHA256
	}
	digest := sha256.Sum256([]byte(id + "\x00" + string(metadata) + "\x00" + parsed.payload.SHA256 + "\x00" + screenshot))
	return service.persistManualSave(ctx, id, key, hex.EncodeToString(digest[:]), launch, parsed)
}

func (service *Service) persistManualSave(ctx context.Context, id, key, digest string,
	launch Launch, parsed parsedManual,
) (ManualResult, bool, error) {
	var result ManualResult
	var replayed bool
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		now := service.now().UnixMilli()
		var err error
		result, replayed, err = replayManualSave(ctx, scope.Idempotency,
			ReplayKey{PrincipalID: launch.PrincipalID, Key: key, AtMS: now}, digest)
		if err != nil || replayed {
			return err
		}
		if err := service.ensureWritable(ctx, scope.Launches, id, launch, parsed.payload.Size); err != nil {
			return err
		}
		payloadID, err := scope.Blobs.Ensure(ctx, parsed.payload, "application/octet-stream", now)
		if err != nil {
			return fmt.Errorf("register checkpoint payload: %w", err)
		}
		if launch.Purpose == "PRODUCT" {
			result, err = service.persistProductCheckpoint(ctx, scope, id, launch, parsed, payloadID, now)
		} else {
			result = ManualResult{
				ResourceKind: "REVIEW_PREVIEW_CHECKPOINT", PreviewID: id,
				CheckpointFormat: launch.Checkpoint.WriteFormat, CreatedAtMS: now,
			}
			err = scope.Checkpoints.ReplacePreview(ctx, PreviewWrite{
				PreviewID: id, PayloadID: payloadID,
				Format: launch.Checkpoint.WriteFormat, AtMS: now,
			})
		}
		if err != nil {
			return fmt.Errorf("persist checkpoint: %w", err)
		}
		body, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("encode checkpoint response: %w", err)
		}
		if err := scope.Idempotency.Remember(ctx, ReplayWrite{
			ReplayKey: ReplayKey{PrincipalID: launch.PrincipalID, Key: key, AtMS: now},
			Replay:    Replay{Digest: digest, Body: body}, ExpiresAtMS: now + int64(24*time.Hour/time.Millisecond),
		}); err != nil {
			return fmt.Errorf("remember checkpoint response: %w", err)
		}
		return nil
	})
	if err != nil {
		return ManualResult{}, false, fmt.Errorf("commit checkpoint: %w", err)
	}
	return result, replayed, nil
}

func replayManualSave(ctx context.Context, records IdempotencyRecords, key ReplayKey,
	digest string,
) (ManualResult, bool, error) {
	previous, found, err := records.Replay(ctx, key)
	if err != nil {
		return ManualResult{}, false, fmt.Errorf("read checkpoint replay: %w", err)
	}
	if !found {
		return ManualResult{}, false, nil
	}
	if subtle.ConstantTimeCompare([]byte(previous.Digest), []byte(digest)) != 1 {
		return ManualResult{}, false, ErrSequenceReused
	}
	var result ManualResult
	if err := json.Unmarshal(previous.Body, &result); err != nil {
		return ManualResult{}, false, fmt.Errorf("decode checkpoint replay: %w", err)
	}
	return result, true, nil
}
