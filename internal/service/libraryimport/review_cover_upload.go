package libraryimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"retrom/internal/hasheous"

	"github.com/google/uuid"
)

type ReviewCoverUploads struct {
	repository ReviewCoverRepository
	blobs      ReviewCoverBlobs
	now        func() time.Time
	newID      func() (string, error)
}

func NewReviewCoverUploads(
	repository ReviewCoverRepository, blobs ReviewCoverBlobs, now func() time.Time,
) *ReviewCoverUploads {
	return &ReviewCoverUploads{repository: repository, blobs: blobs, now: now, newID: newReviewCoverID}
}

func newReviewCoverID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate review cover ID: %w", err)
	}
	return id.String(), nil
}

func (service *ReviewCoverUploads) Upload(ctx context.Context, request ReviewCoverRequest) (ReviewCoverResult, error) {
	if request.Kind != "COVER" || request.ItemID == "" || request.UploadFileID == "" || request.ExpectedVersion < 1 {
		return ReviewCoverResult{}, ErrReviewCoverUploadInvalid
	}
	source, found, err := service.repository.Source(ctx, request.UploadFileID)
	if err != nil {
		return ReviewCoverResult{}, fmt.Errorf("read review cover source: %w", err)
	}
	if !found {
		return ReviewCoverResult{}, ErrReviewCoverUploadInvalid
	}
	if source.Purpose != "GENERAL" {
		return ReviewCoverResult{}, ErrReviewCoverConsumed
	}
	prepared, err := service.prepare(ctx, source)
	if err != nil {
		return ReviewCoverResult{}, err
	}
	var result ReviewCoverResult
	err = service.repository.WithWrite(ctx, func(scope ReviewCoverScope) error {
		record, saveErr := service.save(ctx, scope, request, source, prepared)
		if saveErr != nil {
			return saveErr
		}
		result = ReviewCoverResult{
			AssetID: record.ID, Kind: "COVER", Width: record.Width, Height: record.Height,
			MediaType: record.MediaType, CreatedAtMS: record.CreatedAtMS, Version: request.ExpectedVersion,
		}
		return nil
	})
	if err != nil {
		return ReviewCoverResult{}, fmt.Errorf("commit review cover upload: %w", err)
	}
	return result, nil
}

func (service *ReviewCoverUploads) prepare(ctx context.Context, source ReviewCoverSource) (hasheous.AssetData, error) {
	if err := ctx.Err(); err != nil {
		return hasheous.AssetData{}, fmt.Errorf("prepare review cover: %w", err)
	}
	file, err := service.blobs.OpenDigest(source.Digest)
	if err != nil {
		return hasheous.AssetData{}, fmt.Errorf("%w: open: %w", ErrReviewCoverCASUnavailable, err)
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return hasheous.AssetData{}, fmt.Errorf("%w: read: %w", ErrReviewCoverCASUnavailable, err)
	}
	if err := ctx.Err(); err != nil {
		return hasheous.AssetData{}, fmt.Errorf("prepare review cover: %w", err)
	}
	prepared, err := hasheous.ValidateImage(contents, "")
	if err != nil {
		return hasheous.AssetData{}, fmt.Errorf("%w: %w", ErrReviewCoverImageInvalid, err)
	}
	return prepared, nil
}

func (service *ReviewCoverUploads) save(
	ctx context.Context, scope ReviewCoverScope, request ReviewCoverRequest,
	source ReviewCoverSource, prepared hasheous.AssetData,
) (ReviewCoverRecord, error) {
	if err := checkReviewCoverAuthority(ctx, scope.Reader, request, source); err != nil {
		return ReviewCoverRecord{}, err
	}
	existing, found, err := scope.Reader.ExistingByUpload(ctx, source.FileID)
	if err != nil {
		return ReviewCoverRecord{}, fmt.Errorf("read review cover ownership: %w", err)
	}
	if found {
		if existing.Record.ItemID != request.ItemID {
			return ReviewCoverRecord{}, ErrReviewCoverConsumed
		}
		if !existing.HasConsumption || existing.Record.BlobID != source.BlobID {
			return ReviewCoverRecord{}, ErrReviewCoverIntegrity
		}
		return existing.Record, nil
	}
	assetID, err := service.newID()
	if err != nil {
		return ReviewCoverRecord{}, fmt.Errorf("create review asset ID: %w", err)
	}
	consumptionID, err := service.newID()
	if err != nil {
		return ReviewCoverRecord{}, fmt.Errorf("create review consumption ID: %w", err)
	}
	record := ReviewCoverRecord{
		ID: assetID, ItemID: request.ItemID, UploadFileID: source.FileID, BlobID: source.BlobID,
		Width: prepared.Width, Height: prepared.Height, MediaType: prepared.MediaType, CreatedAtMS: service.now().UnixMilli(),
	}
	if err := scope.Writer.InsertAsset(ctx, record); err != nil {
		return ReviewCoverRecord{}, fmt.Errorf("save review cover asset: %w", err)
	}
	if err := scope.Writer.Consume(ctx, ReviewCoverConsumption{
		ID: consumptionID, UploadID: source.UploadID, FileID: source.FileID,
		AssetID: assetID, CreatedAtMS: record.CreatedAtMS,
	}); err != nil {
		return ReviewCoverRecord{}, fmt.Errorf("retain review cover upload: %w", err)
	}
	return record, nil
}

func checkReviewCoverAuthority(
	ctx context.Context, reader ReviewCoverReader, request ReviewCoverRequest, prepared ReviewCoverSource,
) error {
	current, found, err := reader.Source(ctx, request.UploadFileID)
	if err != nil {
		return fmt.Errorf("recheck review cover source: %w", err)
	}
	if !found || current != prepared {
		return ErrReviewCoverUploadInvalid
	}
	draft, found, err := reader.Draft(ctx, request.ItemID)
	if err != nil {
		return fmt.Errorf("read review cover authority: %w", err)
	}
	if !found || draft.Version != request.ExpectedVersion || draft.State != "REVIEW_PENDING" ||
		draft.SourceBusy {
		return ErrReviewCoverVersion
	}
	return nil
}
