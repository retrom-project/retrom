package saves

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"retrom/internal/foundation/cleanup"
	model "retrom/internal/model/saves"
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
	restore, err := service.repository.Restore(ctx, launchID)
	if err != nil {
		return "", fmt.Errorf("read checkpoint restore: %w", err)
	}
	if !validRestore(restore) {
		return "", model.ErrCheckpointIncompatible
	}
	maximum := min(restore.Checkpoint.MaxBytes, maxStoredCheckpointBytes)
	if _, err := service.readRestorePayload(restore.Digest, maximum, restore.Size); err != nil {
		return "", err
	}
	return restore.Digest, nil
}

func (service *Service) readRestorePayload(digest string, maximum, expectedSize int64) ([]byte, error) {
	file, err := service.blobs.OpenDigest(digest)
	if err != nil {
		return nil, model.ErrCheckpointIncompatible
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	contents, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(contents)) != expectedSize {
		return nil, model.ErrCheckpointIncompatible
	}
	actualDigest := sha256.Sum256(contents)
	if hex.EncodeToString(actualDigest[:]) != digest {
		return nil, model.ErrCheckpointIncompatible
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
