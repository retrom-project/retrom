package saves

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	model "retrom/internal/model/saves"

	"github.com/google/uuid"
)

func (service *Service) CreateManual(ctx context.Context, id, capability, key string,
	request ManualUpload,
) (model.ManualResult, bool, error) {
	launch, err := service.launch(ctx, id, capability)
	if err != nil {
		return model.ManualResult{}, false, err
	}
	return service.createManualForLaunch(ctx, id, key, request, launch)
}

func (service *Service) createManualForLaunch(ctx context.Context, id, key string,
	request ManualUpload, launch model.Launch,
) (model.ManualResult, bool, error) {
	parsed, err := service.parseManual(request, launch)
	if err != nil {
		return model.ManualResult{}, false, err
	}
	metadata, _ := json.Marshal(parsed.metadata)
	screenshot := ""
	if parsed.screenshot != nil {
		screenshot = parsed.screenshot.SHA256
	}
	digest := sha256.Sum256([]byte(id + "\x00" + string(metadata) + "\x00" + parsed.payload.SHA256 + "\x00" + screenshot))

	saveStateID := ""
	if launch.Purpose == "PRODUCT" {
		generated, err := uuid.NewV7()
		if err != nil {
			return model.ManualResult{}, false, fmt.Errorf("generate save identifier: %w", err)
		}
		saveStateID = generated.String()
	}

	now := service.now().UnixMilli()
	cmd := model.ManualCheckpointCommand{
		LaunchID:    id,
		PrincipalID: launch.PrincipalID,
		IdempotencyKey: model.ReplayKey{
			PrincipalID: launch.PrincipalID,
			Key:         key,
			AtMS:        now,
		},
		Digest:              hex.EncodeToString(digest[:]),
		ExpiresAtMS:         now + int64(24*time.Hour/time.Millisecond),
		Launch:              launch,
		Payload:             model.ManualCheckpointPayload{SHA256: parsed.payload.SHA256, MediaType: "application/octet-stream", Size: parsed.payload.Size},
		SaveStateID:         saveStateID,
		MetadataName:        parsed.metadata.Name,
		MetadataDiscIndex:   parsed.metadata.DiscIndex,
		ScreenshotMediaType: parsed.screenshotMediaType,
	}
	if parsed.screenshot != nil {
		cmd.Screenshot = &model.ManualCheckpointImage{
			SHA256:    parsed.screenshot.SHA256,
			MediaType: parsed.screenshotMediaType,
			Size:      parsed.screenshot.Size,
		}
	}

	result, replayed, err := service.repository.CommitManualCheckpoint(ctx, cmd)
	if err != nil {
		return model.ManualResult{}, false, fmt.Errorf("commit checkpoint: %w", err)
	}
	return result, replayed, nil
}
