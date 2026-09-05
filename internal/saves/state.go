package saves

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"retrom/internal/cleanup"
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
	launch, digest, expectedSize, err := loadLaunchForRestore(
		ctx, service.database, launchID,
	)
	if err != nil {
		if errors.Is(err, ErrCheckpointIncompatible) {
			return "", err
		}
		return "", fmt.Errorf("saves/service: %w", err)
	}
	_, err = service.readRestorePayload(digest, launch.checkpointMaxBytes, expectedSize)
	if err != nil {
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

func (service *Service) CheckpointStatus(
	ctx context.Context, launchID, capability string,
) (CheckpointStatus, error) {
	launch, err := service.launch(ctx, launchID, capability)
	if err != nil {
		return CheckpointStatus{}, err
	}
	return CheckpointStatus{
		CheckpointFormat: launch.checkpointFormat,
		Availability:     CheckpointAvailability{Available: true},
	}, nil
}
