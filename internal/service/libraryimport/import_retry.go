package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	model "retrom/internal/model/libraryimport"

	"github.com/google/uuid"
)

type ImportItemRetries struct {
	repository model.ImportItemRetryRepository
	now        func() time.Time
	newID      func() (string, error)
}

func NewImportItemRetries(repository model.ImportItemRetryRepository, now func() time.Time) *ImportItemRetries {
	if now == nil {
		now = time.Now
	}
	return &ImportItemRetries{repository: repository, now: now, newID: newImportRetryID}
}

func newImportRetryID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("allocate import retry job: %w", err)
	}
	return id.String(), nil
}

func (service *ImportItemRetries) Retry(
	ctx context.Context, request model.ImportItemRetryRequest,
) (model.ImportItemRetryResult, error) {
	if strings.TrimSpace(request.ItemID) == "" || request.ExpectedVersion < 1 {
		return model.ImportItemRetryResult{}, model.ErrInvalid
	}
	jobID, err := service.newID()
	if err != nil {
		return model.ImportItemRetryResult{}, err
	}
	now := service.now().UnixMilli()
	snapshot, found, err := service.repository.LoadRetrySnapshot(ctx, request.ItemID)
	if err != nil {
		return model.ImportItemRetryResult{}, fmt.Errorf("retry import item: %w", err)
	}
	if !found || snapshot.State != "FAILED_RETRYABLE" || snapshot.Version != request.ExpectedVersion {
		return model.ImportItemRetryResult{}, model.ErrInvalid
	}
	dedupeInput := request.ItemID + ":" + snapshot.Stage + ":" +
		time.UnixMilli(now).UTC().Format(time.RFC3339Nano)
	dedupe := sha256.Sum256([]byte(dedupeInput))
	payload, err := json.Marshal(map[string]string{"sourceManifestDigest": snapshot.ManifestDigest})
	if err != nil {
		return model.ImportItemRetryResult{}, fmt.Errorf("retry import item: %w", err)
	}
	if err := service.repository.CommitRetry(ctx, model.ImportItemRetryWrite{
		ItemID: request.ItemID, ImportID: snapshot.ImportID, Stage: snapshot.Stage,
		ManifestDigest: snapshot.ManifestDigest, ExpectedVersion: request.ExpectedVersion,
		JobID: jobID, DedupeKey: hex.EncodeToString(dedupe[:]), PayloadJSON: string(payload), NowMS: now,
	}); err != nil {
		return model.ImportItemRetryResult{}, fmt.Errorf("retry import item: %w", err)
	}
	return model.ImportItemRetryResult{
		ItemID: request.ItemID, JobID: jobID, State: "QUEUED", Version: request.ExpectedVersion + 1,
	}, nil
}
