package gamecontent

import (
	"context"
	"fmt"

	model "retrom/internal/model/gamecontent"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/security/authn"
	"retrom/internal/foundation/cleanup"
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
		return Scheduled{}, false, model.ErrInvalid
	}
	now := service.now().UnixMilli()
	principal, _ := authn.PrincipalFromContext(ctx)
	principalID := principal.UserID
	if principalID == "" {
		principalID = "SYSTEM"
	}
	result, err := service.repository.CommitSchedule(ctx, model.ScheduleCommand{
		GameID: gameID, UploadID: uploadID, ContentMode: mode,
		ExpectedVersion: expectedVersion, NowMS: now,
		PrincipalID: principalID, Key: key, Digest: digest,
		MultiDiscImportEnabled: service.multiDiscImportEnabled,
	})
	if err != nil {
		return Scheduled{}, false, fmt.Errorf("schedule content replacement: %w", err)
	}
	scheduled := Scheduled{
		GameID: result.GameID, JobID: result.JobID,
		State: result.State, Version: result.Version,
	}
	if !result.Replayed {
		go func() {
			cleanup.Error("run content replacement", service.Run(context.WithoutCancel(ctx), result.JobID, 1))
		}()
	}
	return scheduled, result.Replayed, nil
}

// ValidateUpload checks an upload state for content replacement eligibility.
func ValidateUpload(upload model.Upload, mode, platformID string) error {
	if upload.State != "COMPLETE" || upload.FileCount == 0 || upload.Consumptions != 0 {
		return model.ErrInvalid
	}
	if mode == contentcapability.ModeStandard && platformID != "dos" && upload.FileCount != 1 {
		return model.ErrInvalid
	}
	if (mode == contentcapability.ModeMultiDisc || mode == contentcapability.ModeRPGMakerProject) &&
		upload.SourceType != "DIRECTORY" {
		return model.ErrInvalid
	}
	return nil
}
