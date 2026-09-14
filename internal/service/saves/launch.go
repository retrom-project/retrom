package saves

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	retromruntime "retrom/internal/adapter/runtime/runtime"
)

const maxStoredCheckpointBytes = int64(256 << 20)

func (service *Service) launch(ctx context.Context, id, capability string) (Launch, error) {
	launch, err := service.loadLaunch(ctx, id)
	if err != nil {
		return Launch{}, err
	}
	if !validLaunchAccess(capability, launch, service.now().UnixMilli()) {
		return Launch{}, ErrCredential
	}
	return launch, nil
}

func (service *Service) loadLaunch(ctx context.Context, id string) (Launch, error) {
	launch, err := service.repository.LoadLaunch(ctx, id)
	if err != nil {
		return Launch{}, fmt.Errorf("load checkpoint launch: %w", err)
	}
	if err := validateLaunch(launch); err != nil {
		return Launch{}, err
	}
	return launch, nil
}

func validateLaunch(launch Launch) error {
	if launch.Checkpoint.WriteFormat == "" || launch.Checkpoint.MaxBytes < 1 {
		return ErrCheckpointUnavailable
	}
	if !validLaunchDiscShape(launch) {
		return ErrCredential
	}
	return nil
}

func validLaunchAccess(capability string, launch Launch, now int64) bool {
	return retromruntime.MatchesCapability(capability, launch.CredentialHash) &&
		launch.State == "ACTIVE" && launch.HardExpiresAtMS > now
}

func validLaunchDiscShape(launch Launch) bool {
	if launch.ContentFormat != "RETROM_MULTIDISC_M3U_V1" {
		return launch.DiscCount == 0 && launch.InitialDiscIndex == 0
	}
	return launch.DiscCount >= 2 && launch.InitialDiscIndex >= 0 && launch.InitialDiscIndex < launch.DiscCount
}

func (service *Service) ensureWritable(ctx context.Context, records LaunchReader, id string,
	expected Launch, payloadSize int64,
) error {
	current, err := records.LoadLaunch(ctx, id)
	if err != nil {
		return fmt.Errorf("read checkpoint write authority: %w", err)
	}
	if err := validateLaunch(current); err != nil {
		return err
	}
	if !sameCheckpointOwner(current, expected) {
		return ErrCredential
	}
	if expected.LocalDraft {
		if !localDraftWritable(current) {
			return ErrCredential
		}
	} else if current.State != "ACTIVE" || current.HardExpiresAtMS <= service.now().UnixMilli() ||
		!bytes.Equal(current.CredentialHash, expected.CredentialHash) {
		return ErrCredential
	}
	if current.Purpose == "PRODUCT" {
		if current.GameStatus != "PUBLISHED" {
			return ErrCredential
		}
	} else if current.ItemState != "REVIEW_PENDING" || current.PayloadState != "RETAINED" {
		return ErrCredential
	}
	if payloadSize > min(current.Checkpoint.MaxBytes, maxStoredCheckpointBytes) {
		return ErrTooLarge
	}
	return nil
}

func sameCheckpointOwner(current, expected Launch) bool {
	return current.ProfileID == expected.ProfileID && current.PrincipalID == expected.PrincipalID &&
		current.GameID == expected.GameID && current.Purpose == expected.Purpose &&
		current.Checkpoint.WriteFormat == expected.Checkpoint.WriteFormat
}

func validRestore(restore Restore) bool {
	return restore.Size > 0 && restore.Size <= min(restore.Checkpoint.MaxBytes, maxStoredCheckpointBytes) &&
		slices.Contains(restore.Checkpoint.ReadFormats, restore.Format)
}

func localDraftWritable(launch Launch) bool {
	return launch.Purpose == "PRODUCT" && launch.GameStatus == "PUBLISHED" &&
		(launch.State == "ACTIVE" || launch.State == "FINISHED" || launch.State == "EXPIRED") &&
		launch.HasGameSaveBinding && launch.Checkpoint.Semantics == "GAME_SAVE"
}
