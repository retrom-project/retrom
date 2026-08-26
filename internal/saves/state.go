package saves

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"retrom/internal/cleanup"
	"retrom/internal/rpgmaker/checkpoint"
)

func (service *Service) StateDigest(ctx context.Context, launchID, capability string) (string, error) {
	if _, err := service.launch(ctx, launchID, capability); err != nil {
		return "", err
	}
	return service.stateDigestAuthorized(ctx, launchID)
}

func (service *Service) IsolatedStateDigest(ctx context.Context, launchID string) (string, error) {
	return service.stateDigestAuthorized(ctx, launchID)
}

func (service *Service) stateDigestAuthorized(ctx context.Context, launchID string) (string, error) {
	launch, digest, nativeProfile, resumeSlot, expectedSize, err := loadLaunchForRestore(
		ctx, service.database, launchID,
	)
	if err != nil {
		if errors.Is(err, ErrCheckpointIncompatible) {
			return "", err
		}
		return "", fmt.Errorf("saves/service: %w", err)
	}
	contents, err := service.readRestorePayload(digest, launch.payloadMaxBytes, expectedSize)
	if err != nil {
		return "", err
	}
	if err := validateRestoreContents(launch, contents, nativeProfile, resumeSlot); err != nil {
		return "", err
	}
	return digest, nil
}

func (service *Service) readRestorePayload(digest string, maximum, expectedSize int64) ([]byte, error) {
	file, err := service.blobs.OpenDigest(digest)
	if err != nil {
		return nil, ErrCheckpointIncompatible
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	contents, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(contents)) != expectedSize {
		return nil, ErrCheckpointIncompatible
	}
	actualDigest := sha256.Sum256(contents)
	if hex.EncodeToString(actualDigest[:]) != digest {
		return nil, ErrCheckpointIncompatible
	}
	return contents, nil
}

func validateRestoreContents(
	launch launchSnapshot,
	contents []byte,
	nativeProfile sql.NullString,
	resumeSlot sql.NullInt64,
) error {
	if launch.payloadKind == "NATIVE_SAVE_BUNDLE_V1" {
		binding, err := validateNativeContents(launch.generation, contents)
		if err != nil {
			return err
		}
		if !nativeProfile.Valid || !resumeSlot.Valid || binding.profile == nil || binding.slot == nil ||
			nativeProfile.String != *binding.profile || resumeSlot.Int64 != *binding.slot {
			return ErrCheckpointIncompatible
		}
	} else if nativeProfile.Valid || resumeSlot.Valid {
		return ErrCheckpointIncompatible
	}
	return nil
}

func validateNativeContents(generation string, contents []byte) (nativeBinding, error) {
	bundle, err := checkpoint.Decode(contents)
	if err != nil {
		return nativeBinding{}, ErrCheckpointInvalid
	}
	profile, engine, valid := expectedNativeProfile(generation)
	if !valid || bundle.Manifest.Engine != engine {
		return nativeBinding{}, ErrCheckpointIncompatible
	}
	slot := bundle.Manifest.ResumeSlot
	return nativeBinding{profile: &profile, slot: &slot}, nil
}

func (service *Service) CheckpointStatus(
	ctx context.Context, launchID, capability string,
) (CheckpointStatus, error) {
	launch, err := service.launch(ctx, launchID, capability)
	if err != nil {
		return CheckpointStatus{}, err
	}
	status := CheckpointStatus{
		PayloadKind:  launch.payloadKind,
		Availability: CheckpointAvailability{Available: true},
	}
	if launch.purpose == "PRODUCT" {
		return status, nil
	}
	var validationState string
	var checkpointCount int
	err = service.database.QueryRowContext(ctx, `
SELECT validation.state,
 (SELECT count(*) FROM rpgmaker_runtime_validation_checkpoints checkpoint
  WHERE checkpoint.validation_id=validation.id)
FROM rpgmaker_runtime_validations validation
WHERE validation.id=?
`, launch.validationID).Scan(&validationState, &checkpointCount)
	if errors.Is(err, sql.ErrNoRows) {
		return CheckpointStatus{}, ErrCredential
	}
	if err != nil {
		return CheckpointStatus{}, fmt.Errorf("saves/service: %w", err)
	}
	var reason string
	switch {
	case checkpointCount > 0:
		reason = "CHECKPOINT_ALREADY_CREATED"
	case validationState == "FAILED" || validationState == "EXPIRED":
		reason = "RUNTIME_FAILED"
	case validationState != "RUNNING":
		reason = "RUNTIME_NOT_READY"
	}
	if reason != "" {
		status.Availability.Available = false
		status.Availability.Reason = &reason
	}
	return status, nil
}
