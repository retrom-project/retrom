package saves

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	retromruntime "retrom/internal/adapter/runtime/runtime"
	model "retrom/internal/model/saves"
)

const maxStoredCheckpointBytes = int64(256 << 20)

func (service *Service) launch(ctx context.Context, id, capability string) (model.Launch, error) {
	launch, err := service.loadLaunch(ctx, id)
	if err != nil {
		return model.Launch{}, err
	}
	if !validLaunchAccess(capability, launch, service.now().UnixMilli()) {
		return model.Launch{}, model.ErrCredential
	}
	return launch, nil
}

func (service *Service) loadLaunch(ctx context.Context, id string) (model.Launch, error) {
	launch, err := service.repository.LoadLaunch(ctx, id)
	if err != nil {
		return model.Launch{}, fmt.Errorf("load checkpoint launch: %w", err)
	}
	if err := validateLaunch(launch); err != nil {
		return model.Launch{}, err
	}
	return launch, nil
}

func validateLaunch(launch model.Launch) error {
	if launch.Checkpoint.WriteFormat == "" || launch.Checkpoint.MaxBytes < 1 {
		return model.ErrCheckpointUnavailable
	}
	if !validLaunchDiscShape(launch) {
		return model.ErrCredential
	}
	return nil
}

func validLaunchAccess(capability string, launch model.Launch, now int64) bool {
	return retromruntime.MatchesCapability(capability, launch.CredentialHash) &&
		launch.State == "ACTIVE" && launch.HardExpiresAtMS > now
}

func validLaunchDiscShape(launch model.Launch) bool {
	if launch.ContentFormat != "RETROM_MULTIDISC_M3U_V1" {
		return launch.DiscCount == 0 && launch.InitialDiscIndex == 0
	}
	return launch.DiscCount >= 2 && launch.InitialDiscIndex >= 0 && launch.InitialDiscIndex < launch.DiscCount
}

func (service *Service) ensureWritable(ctx context.Context, records model.LaunchReader, id string,
	expected model.Launch, payloadSize int64,
) error {
	current, err := records.LoadLaunch(ctx, id)
	if err != nil {
		return fmt.Errorf("read checkpoint write authority: %w", err)
	}
	if err := validateLaunch(current); err != nil {
		return err
	}
	if !sameCheckpointOwner(current, expected) {
		return model.ErrCredential
	}
	if expected.LocalDraft {
		if !localDraftWritable(current) {
			return model.ErrCredential
		}
	} else if current.State != "ACTIVE" || current.HardExpiresAtMS <= service.now().UnixMilli() ||
		!bytes.Equal(current.CredentialHash, expected.CredentialHash) {
		return model.ErrCredential
	}
	if current.Purpose == "PRODUCT" {
		if current.GameStatus != "PUBLISHED" {
			return model.ErrCredential
		}
	} else if current.ItemState != "REVIEW_PENDING" || current.PayloadState != "RETAINED" {
		return model.ErrCredential
	}
	if payloadSize > min(current.Checkpoint.MaxBytes, maxStoredCheckpointBytes) {
		return model.ErrTooLarge
	}
	return nil
}

func sameCheckpointOwner(current, expected model.Launch) bool {
	return current.ProfileID == expected.ProfileID && current.PrincipalID == expected.PrincipalID &&
		current.GameID == expected.GameID && current.Purpose == expected.Purpose &&
		current.Checkpoint.WriteFormat == expected.Checkpoint.WriteFormat
}

func validRestore(restore model.Restore) bool {
	return restore.Size > 0 && restore.Size <= min(restore.Checkpoint.MaxBytes, maxStoredCheckpointBytes) &&
		slices.Contains(restore.Checkpoint.ReadFormats, restore.Format)
}

func localDraftWritable(launch model.Launch) bool {
	return launch.Purpose == "PRODUCT" && launch.GameStatus == "PUBLISHED" &&
		(launch.State == "ACTIVE" || launch.State == "FINISHED" || launch.State == "EXPIRED") &&
		launch.HasGameSaveBinding && launch.Checkpoint.Semantics == "GAME_SAVE"
}
