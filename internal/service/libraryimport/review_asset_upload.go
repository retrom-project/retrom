package libraryimport

import (
	"context"
	"fmt"
	"time"

	"retrom/internal/service/idempotency"

	"retrom/internal/filestore"

	"github.com/google/uuid"
)

type ReviewAssetUploads struct {
	repository ReviewAssetRepository
	blobs      ReviewAssetBlobs
	now        func() time.Time
	newID      func() (string, error)
}

func NewReviewAssetUploads(
	repository ReviewAssetRepository, blobs ReviewAssetBlobs, now func() time.Time,
) *ReviewAssetUploads {
	return &ReviewAssetUploads{repository: repository, blobs: blobs, now: now, newID: newReviewAssetID}
}

func newReviewAssetID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate review asset ID: %w", err)
	}
	return id.String(), nil
}

func (service *ReviewAssetUploads) Upload(ctx context.Context, request ReviewAssetRequest) (ReviewAssetResult, error) {
	if (request.Kind != "COVER" && request.Kind != "VIDEO") ||
		request.ItemID == "" || request.UploadFileID == "" || request.ExpectedVersion < 1 {
		return ReviewAssetResult{}, ErrReviewAssetUploadInvalid
	}
	source, found, err := service.repository.Source(ctx, request.UploadFileID)
	if err != nil {
		return ReviewAssetResult{}, fmt.Errorf("read review asset source: %w", err)
	}
	if !found {
		return ReviewAssetResult{}, ErrReviewAssetUploadInvalid
	}
	if source.Purpose != "GENERAL" {
		return ReviewAssetResult{}, ErrReviewAssetConsumed
	}
	prepared, err := service.prepare(ctx, source, request.Kind)
	if err != nil {
		return ReviewAssetResult{}, err
	}
	file, err := service.blobs.CopyTo(ctx, source.FileRecord,
		filestore.ItemDirectory(request.ItemID)+"/scratch/media/"+source.FileID, "asset")
	if err != nil {
		return ReviewAssetResult{}, fmt.Errorf("upload: %w", err)
	}
	value, err := filestore.FileRecord(file, prepared.MediaType)
	if err != nil {
		return ReviewAssetResult{}, fmt.Errorf("upload: %w", err)
	}
	var result ReviewAssetResult
	err = service.repository.WithWrite(ctx, func(scope ReviewAssetScope) error {
		record, saveErr := service.save(ctx, scope, request, source, prepared, value)
		if saveErr != nil {
			return saveErr
		}
		result = ReviewAssetResult{
			AssetID: record.ID, Kind: record.Kind, Width: record.Width, Height: record.Height,
			MediaType: record.MediaType, CreatedAtMS: record.CreatedAtMS, Version: request.ExpectedVersion,
		}
		return idempotency.Complete(ctx, idempotency.Result{Value: result, Version: result.Version})
	})
	if err != nil {
		return ReviewAssetResult{}, fmt.Errorf("commit review asset upload: %w", err)
	}
	return result, nil
}

func (service *ReviewAssetUploads) save(
	ctx context.Context, scope ReviewAssetScope, request ReviewAssetRequest,
	source ReviewAssetSource, prepared preparedReviewAsset, value string,
) (ReviewAssetRecord, error) {
	if err := checkReviewAssetAuthority(ctx, scope.Reader, request, source); err != nil {
		return ReviewAssetRecord{}, err
	}
	existing, found, err := scope.Reader.ExistingByUpload(ctx, source.FileID)
	if err != nil {
		return ReviewAssetRecord{}, fmt.Errorf("read review asset ownership: %w", err)
	}
	if found {
		if existing.Record.ItemID != request.ItemID || existing.Record.Kind != request.Kind {
			return ReviewAssetRecord{}, ErrReviewAssetConsumed
		}
		if !existing.HasConsumption || existing.Record.FileRecord != value {
			return ReviewAssetRecord{}, ErrReviewAssetIntegrity
		}
		return existing.Record, nil
	}
	assetID, err := service.newID()
	if err != nil {
		return ReviewAssetRecord{}, fmt.Errorf("create review asset ID: %w", err)
	}
	consumptionID, err := service.newID()
	if err != nil {
		return ReviewAssetRecord{}, fmt.Errorf("create review consumption ID: %w", err)
	}
	record := ReviewAssetRecord{
		ID: assetID, Kind: request.Kind, ItemID: request.ItemID, UploadFileID: source.FileID, FileRecord: value,
		Width: prepared.Width, Height: prepared.Height, MediaType: prepared.MediaType, CreatedAtMS: service.now().UnixMilli(),
	}
	if err := scope.Writer.InsertAsset(ctx, record); err != nil {
		return ReviewAssetRecord{}, fmt.Errorf("save review asset asset: %w", err)
	}
	if err := scope.Writer.Consume(ctx, ReviewAssetConsumption{
		ID: consumptionID, UploadID: source.UploadID, FileID: source.FileID,
		AssetID: assetID, CreatedAtMS: record.CreatedAtMS,
	}); err != nil {
		return ReviewAssetRecord{}, fmt.Errorf("retain review asset upload: %w", err)
	}
	return record, nil
}

func checkReviewAssetAuthority(
	ctx context.Context, reader ReviewAssetReader, request ReviewAssetRequest, prepared ReviewAssetSource,
) error {
	current, found, err := reader.Source(ctx, request.UploadFileID)
	if err != nil {
		return fmt.Errorf("recheck review asset source: %w", err)
	}
	if !found || current != prepared {
		return ErrReviewAssetUploadInvalid
	}
	draft, found, err := reader.Draft(ctx, request.ItemID)
	if err != nil {
		return fmt.Errorf("read review asset authority: %w", err)
	}
	if !found || draft.Version != request.ExpectedVersion || draft.State != "REVIEW_PENDING" ||
		draft.SourceBusy {
		return ErrReviewAssetVersion
	}
	return nil
}
