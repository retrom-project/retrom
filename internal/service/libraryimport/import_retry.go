package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ImportItemRetries struct {
	repository ImportItemRetryRepository
	now        func() time.Time
	newID      func() (string, error)
}

func NewImportItemRetries(repository ImportItemRetryRepository, now func() time.Time) *ImportItemRetries {
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
	ctx context.Context, request ImportItemRetryRequest,
) (ImportItemRetryResult, error) {
	if strings.TrimSpace(request.ItemID) == "" || request.ExpectedVersion < 1 {
		return ImportItemRetryResult{}, ErrInvalid
	}
	jobID, err := service.newID()
	if err != nil {
		return ImportItemRetryResult{}, err
	}
	now := service.now().UnixMilli()
	var result ImportItemRetryResult
	err = service.repository.WithRetry(ctx, func(scope ImportItemRetryScope) error {
		snapshot, found, err := scope.Current(ctx, request.ItemID)
		if err != nil {
			return fmt.Errorf("read import item retry: %w", err)
		}
		if !found || snapshot.State != "FAILED_RETRYABLE" || snapshot.Version != request.ExpectedVersion {
			return ErrInvalid
		}
		dedupeInput := request.ItemID + ":" + snapshot.Stage + ":" +
			time.UnixMilli(now).UTC().Format(time.RFC3339Nano)
		dedupe := sha256.Sum256([]byte(dedupeInput))
		payload, err := json.Marshal(map[string]string{"sourceManifestDigest": snapshot.ManifestDigest})
		if err != nil {
			return fmt.Errorf("encode import retry input: %w", err)
		}
		if err := scope.Retry(ctx, ImportItemRetryWrite{
			ItemID: request.ItemID, ImportID: snapshot.ImportID, Stage: snapshot.Stage,
			ManifestDigest: snapshot.ManifestDigest, ExpectedVersion: request.ExpectedVersion,
			JobID: jobID, DedupeKey: hex.EncodeToString(dedupe[:]), PayloadJSON: string(payload), NowMS: now,
		}); err != nil {
			return fmt.Errorf("persist import item retry: %w", err)
		}
		result = ImportItemRetryResult{
			ItemID: request.ItemID, JobID: jobID, State: "QUEUED", Version: request.ExpectedVersion + 1,
		}
		return nil
	})
	if err != nil {
		return ImportItemRetryResult{}, fmt.Errorf("retry import item: %w", err)
	}
	return result, nil
}
