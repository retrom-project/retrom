package storageanalysis

import (
	"context"
	"fmt"
	"time"
)

type Service struct {
	repository Repository
	now        func() time.Time
}

func New(repository Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func (service *Service) Analyze(ctx context.Context) (Snapshot, error) {
	source, err := service.repository.Read(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("storageanalysis: read snapshot: %w", err)
	}
	snapshot, err := aggregate(source.Blobs, source.Retained, source.Usage)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Details, err = details(source)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Scope = Scope
	snapshot.GeneratedAtMS = service.now().UnixMilli()
	snapshot.Excluded = append([]string(nil), Excluded[:]...)
	return snapshot, nil
}

func details(source ReadModel) (Details, error) {
	result := Details{
		SaveStates: SaveStateDetails{
			ActiveCount:  source.Saves.ActiveCount,
			DeletedCount: source.Saves.DeletedCount,
		},
	}
	var err error
	result.SaveStates.StateBytes, err = referenceBytes(source.Saves.PayloadIDs, source.Blobs, errSaveBlobMissing)
	if err != nil {
		return Details{}, err
	}
	result.SaveStates.ScreenshotBytes, err = referenceBytes(
		source.Saves.ScreenshotIDs,
		source.Blobs,
		errSaveBlobMissing,
	)
	if err != nil {
		return Details{}, err
	}
	result.CleanupCandidates.Bytes, err = referenceBytes(
		source.CleanupCandidates,
		source.Blobs,
		errCandidateBlobMissing,
	)
	if err != nil {
		return Details{}, err
	}
	result.CleanupCandidates.FileCount = int64(len(source.CleanupCandidates))
	return result, nil
}

func referenceBytes(ids []string, blobs map[string]int64, missing error) (int64, error) {
	var total int64
	for _, id := range ids {
		size, ok := blobs[id]
		if !ok {
			return 0, fmt.Errorf("storageanalysis: %w", missing)
		}
		var err error
		total, err = addChecked(total, size)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}
