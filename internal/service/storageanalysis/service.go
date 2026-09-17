package storageanalysis

import (
	"context"
	"fmt"
	"log/slog"
	model "retrom/internal/model/storageanalysis"
	"time"
)

type Service struct {
	repository model.Repository
	now        func() time.Time
}

func New(repository model.Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func (service *Service) Analyze(ctx context.Context) (Snapshot, error) {
	source, err := service.repository.Read(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("storageanalysis: read snapshot: %w", err)
	}
	for _, member := range source.Archives {
		if _, protected := source.Protected[member.ArchiveID]; protected {
			source.Usage[member.MemberID] |= source.Usage[member.ArchiveID]
		}
	}
	snapshot, err := aggregate(source.Blobs, source.Protected, source.Usage)
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
	for _, category := range snapshot.Categories {
		if category.Code == CategoryOtherReferenced && category.BlobCount > 0 {
			slog.WarnContext(
				ctx,
				"storage analysis found uncategorized protected blobs",
				"category",
				category.Code,
				"blob_count",
				category.BlobCount,
				"bytes",
				category.Bytes,
			)
		}
	}
	return snapshot, nil
}

func details(source model.ReadModel) (Details, error) {
	result := Details{
		SaveStates: SaveStateDetails{
			ActiveCount:  source.Saves.ActiveCount,
			DeletedCount: source.Saves.DeletedCount,
		},
	}
	var err error
	result.SaveStates.StateReferenceBytes, err = referenceBytes(source.Saves.PayloadIDs, source.Blobs, errSaveBlobMissing)
	if err != nil {
		return Details{}, err
	}
	result.SaveStates.ScreenshotReferenceBytes, err = referenceBytes(
		source.Saves.ScreenshotIDs,
		source.Blobs,
		errSaveBlobMissing,
	)
	if err != nil {
		return Details{}, err
	}
	result.CleanupCandidates.Bytes, err = referenceBytes(source.CleanupCandidates, source.Blobs, errCandidateBlobMissing)
	if err != nil {
		return Details{}, err
	}
	result.CleanupCandidates.BlobCount = int64(len(source.CleanupCandidates))
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
