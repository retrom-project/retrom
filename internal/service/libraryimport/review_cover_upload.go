package libraryimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/adapter/metadata/hasheous"

	"github.com/google/uuid"
)

type ReviewCoverUploads struct {
	repository model.ReviewCoverRepository
	blobs      model.ReviewCoverBlobs
	now        func() time.Time
	newID      func() (string, error)
}

func NewReviewCoverUploads(
	repository model.ReviewCoverRepository, blobs model.ReviewCoverBlobs, now func() time.Time,
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

func (service *ReviewCoverUploads) Upload(
	ctx context.Context,
	request model.ReviewCoverRequest,
) (model.ReviewCoverResult, error) {
	if request.Kind != "COVER" || request.ItemID == "" || request.UploadFileID == "" || request.ExpectedVersion < 1 {
		return model.ReviewCoverResult{}, model.ErrReviewCoverUploadInvalid
	}
	source, found, err := service.repository.Source(ctx, request.UploadFileID)
	if err != nil {
		return model.ReviewCoverResult{}, fmt.Errorf("read review cover source: %w", err)
	}
	if !found {
		return model.ReviewCoverResult{}, model.ErrReviewCoverUploadInvalid
	}
	if source.Purpose != "GENERAL" {
		return model.ReviewCoverResult{}, model.ErrReviewCoverConsumed
	}
	prepared, err := service.prepare(ctx, source)
	if err != nil {
		return model.ReviewCoverResult{}, err
	}
	assetID, err := service.newID()
	if err != nil {
		return model.ReviewCoverResult{}, fmt.Errorf("create review asset ID: %w", err)
	}
	consumptionID, err := service.newID()
	if err != nil {
		return model.ReviewCoverResult{}, fmt.Errorf("create review consumption ID: %w", err)
	}
	record, err := service.repository.CommitCoverUpload(ctx, model.ReviewCoverUploadCommand{
		Request:       request,
		Source:        source,
		AssetID:       assetID,
		ConsumptionID: consumptionID,
		Width:         prepared.Width,
		Height:        prepared.Height,
		MediaType:     prepared.MediaType,
		NowMS:         service.now().UnixMilli(),
	})
	if err != nil {
		return model.ReviewCoverResult{}, fmt.Errorf("commit review cover upload: %w", err)
	}
	return model.ReviewCoverResult{
		AssetID: record.ID, Kind: "COVER", Width: record.Width, Height: record.Height,
		MediaType: record.MediaType, CreatedAtMS: record.CreatedAtMS, Version: request.ExpectedVersion,
	}, nil
}

func (service *ReviewCoverUploads) prepare(
	ctx context.Context,
	source model.ReviewCoverSource,
) (hasheous.AssetData, error) {
	if err := ctx.Err(); err != nil {
		return hasheous.AssetData{}, fmt.Errorf("prepare review cover: %w", err)
	}
	file, err := service.blobs.OpenDigest(source.Digest)
	if err != nil {
		return hasheous.AssetData{}, fmt.Errorf("%w: open: %w", model.ErrReviewCoverCASUnavailable, err)
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return hasheous.AssetData{}, fmt.Errorf("%w: read: %w", model.ErrReviewCoverCASUnavailable, err)
	}
	if err := ctx.Err(); err != nil {
		return hasheous.AssetData{}, fmt.Errorf("prepare review cover: %w", err)
	}
	prepared, err := hasheous.ValidateImage(contents, "")
	if err != nil {
		return hasheous.AssetData{}, fmt.Errorf("%w: %w", model.ErrReviewCoverImageInvalid, err)
	}
	return prepared, nil
}

