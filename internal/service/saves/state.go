package saves

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"retrom/internal/cleanup"
)

func (service *Service) StateFile(ctx context.Context, launchID, capability string) (Restore, error) {
	if _, err := service.launch(ctx, launchID, capability); err != nil {
		return Restore{}, err
	}
	return service.stateFileAuthorized(ctx, launchID)
}

func (service *Service) IsolatedStateFile(ctx context.Context, launchID string) (Restore, error) {
	return service.stateFileAuthorized(ctx, launchID)
}

func (service *Service) stateFileAuthorized(ctx context.Context, launchID string) (Restore, error) {
	restore, err := service.repository.Restore(ctx, launchID)
	if err != nil {
		return Restore{}, fmt.Errorf("read checkpoint restore: %w", err)
	}
	if !validRestore(restore) {
		return Restore{}, ErrCheckpointIncompatible
	}
	maximum := min(restore.Checkpoint.MaxBytes, maxStoredCheckpointBytes)
	if _, err := service.readRestorePayload(restore.BlobID, restore.Digest, maximum, restore.Size); err != nil {
		return Restore{}, err
	}
	return restore, nil
}

func (service *Service) readRestorePayload(id, digest string, maximum, expectedSize int64) ([]byte, error) {
	file, err := service.blobs.OpenID(id)
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

func (service *Service) CheckpointStatus(
	ctx context.Context, launchID, capability string,
) (CheckpointStatus, error) {
	launch, err := service.launch(ctx, launchID, capability)
	if err != nil {
		return CheckpointStatus{}, err
	}
	return CheckpointStatus{
		CheckpointFormat: launch.Checkpoint.WriteFormat,
		Availability:     CheckpointAvailability{Available: true},
	}, nil
}
