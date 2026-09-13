package gameassets

import (
	"context"
	"fmt"

	"retrom/internal/adapter/files/mediaasset"
)

// Prepare validates the completed upload and inspects its immutable CAS bytes.
func (service *Service) Prepare(ctx context.Context, uploadFileID, kind string) (PreparedAsset, error) {
	if uploadFileID == "" || !ValidUpload(kind, 0) {
		return PreparedAsset{}, ErrInvalid
	}
	upload, found, err := service.repository.Upload(ctx, uploadFileID)
	if err != nil {
		return PreparedAsset{}, &ValidationError{
			Code: "ASSET_UPLOAD_INVALID", Message: "上传文件不可用", Cause: err,
		}
	}
	if !found {
		return PreparedAsset{}, &ValidationError{Code: "ASSET_UPLOAD_INVALID", Message: "上传文件不可用"}
	}
	if service.blobs == nil {
		return PreparedAsset{}, &ValidationError{Code: "CAS_UNAVAILABLE", Message: "媒体字节不可用"}
	}
	file, err := service.blobs.OpenDigest(upload.Digest)
	if err != nil {
		return PreparedAsset{}, &ValidationError{Code: "CAS_UNAVAILABLE", Message: "媒体字节不可用", Cause: err}
	}
	defer func() { _ = file.Close() }()
	prepared := PreparedAsset{
		UploadID: upload.UploadID, BlobID: upload.BlobID, Digest: upload.Digest, SizeBytes: upload.SizeBytes,
	}
	if kind == "VIDEO" {
		prepared.MediaType, err = mediaasset.InspectVideo(file, upload.SizeBytes)
		if err != nil {
			return PreparedAsset{}, &ValidationError{
				Code: "ASSET_VIDEO_INVALID", Message: "视频必须是受限 MP4 或 WebM", Cause: err,
			}
		}
		return prepared, nil
	}
	imageData, err := mediaasset.InspectImage(file, upload.SizeBytes)
	if err != nil {
		return PreparedAsset{}, &ValidationError{
			Code: "ASSET_IMAGE_INVALID", Message: "媒体必须是受限 PNG、JPEG 或 WebP", Cause: err,
		}
	}
	prepared.MediaType = imageData.MediaType
	prepared.WidthPX, prepared.HeightPX = &imageData.WidthPX, &imageData.HeightPX
	return prepared, nil
}

// Create replaces one game asset slot and consumes its upload atomically.
func (service *Service) Create(ctx context.Context, request CreateRequest) (CreateResult, error) {
	if request.GameID == "" || request.UploadFileID == "" || !ValidUpload(request.Kind, request.Ordinal) ||
		request.ExpectedVersion < 1 || request.Asset.UploadID == "" || request.Asset.BlobID == "" {
		return CreateResult{}, ErrInvalid
	}
	assetID, err := service.newID()
	if err != nil {
		return CreateResult{}, err
	}
	consumptionID, err := service.newID()
	if err != nil {
		return CreateResult{}, err
	}
	result := CreateResult{
		AssetID: assetID, GameID: request.GameID, Kind: request.Kind, Ordinal: request.Ordinal,
		WidthPX: request.Asset.WidthPX, HeightPX: request.Asset.HeightPX,
		MediaType: request.Asset.MediaType, Version: request.ExpectedVersion + 1, CreatedAtMS: request.NowMS,
	}
	err = service.repository.WithWrite(ctx, func(scope WriteScope) error {
		return service.createInScope(ctx, scope, request, assetID, consumptionID)
	})
	if err != nil {
		return CreateResult{}, fmt.Errorf("create game asset: %w", err)
	}
	return result, nil
}

func (service *Service) createInScope(
	ctx context.Context,
	scope WriteScope,
	request CreateRequest,
	assetID, consumptionID string,
) error {
	version, err := scope.GameVersion(ctx, request.GameID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrVersionConflict, err)
	}
	if version != request.ExpectedVersion {
		return ErrVersionConflict
	}
	replaced, err := scope.RemoveSlot(ctx, request.GameID, request.Kind, request.Ordinal)
	if err != nil {
		return fmt.Errorf("remove replaced game asset: %w", err)
	}
	if err := scope.Create(ctx, AssetRecord{
		ID: assetID, GameID: request.GameID, BlobID: request.Asset.BlobID, Kind: request.Kind,
		Ordinal: request.Ordinal, WidthPX: request.Asset.WidthPX, HeightPX: request.Asset.HeightPX,
		MediaType: request.Asset.MediaType, CreatedAtMS: request.NowMS,
	}); err != nil {
		return fmt.Errorf("create game asset: %w", err)
	}
	if err := scope.ConsumeUpload(ctx, ConsumptionRecord{
		ID: consumptionID, UploadID: request.Asset.UploadID, UploadFileID: request.UploadFileID,
		ConsumerID: assetID, CreatedAtMS: request.NowMS,
	}); err != nil {
		return fmt.Errorf("%w: %w", ErrUploadConsumed, err)
	}
	if err := updateGameAssetVersion(ctx, scope, request); err != nil {
		return err
	}
	if err := scope.StageCandidates(ctx, replaced); err != nil {
		return fmt.Errorf("stage replaced game assets: %w", err)
	}
	if err := scope.ScheduleConsumption(ctx, consumptionID, request.NowMS); err != nil {
		return fmt.Errorf("schedule game asset upload release: %w", err)
	}
	return nil
}

func updateGameAssetVersion(ctx context.Context, scope WriteScope, request CreateRequest) error {
	changed, err := scope.UpdateGame(ctx, request.GameID, request.ExpectedVersion, request.NowMS)
	if err != nil {
		return fmt.Errorf("update game asset version: %w", err)
	}
	if !changed {
		return ErrVersionConflict
	}
	return nil
}

// Delete removes the video slot and stages its old payload atomically.
func (service *Service) Delete(ctx context.Context, request DeleteRequest) (DeleteResult, error) {
	if request.GameID == "" || request.Kind != "VIDEO" || request.ExpectedVersion < 1 {
		return DeleteResult{}, ErrInvalid
	}
	var result DeleteResult
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		version, err := scope.GameVersion(ctx, request.GameID)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrVersionConflict, err)
		}
		if version != request.ExpectedVersion {
			return ErrVersionConflict
		}
		exists, err := scope.AssetExists(ctx, request.GameID, request.Kind)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrAssetNotFound, err)
		}
		if !exists {
			return ErrAssetNotFound
		}
		replaced, err := scope.RemoveSlot(ctx, request.GameID, request.Kind, 0)
		if err != nil {
			return fmt.Errorf("remove game asset: %w", err)
		}
		changed, err := scope.UpdateGame(ctx, request.GameID, request.ExpectedVersion, request.NowMS)
		if err != nil {
			return fmt.Errorf("update game asset version: %w", err)
		}
		if !changed {
			return ErrVersionConflict
		}
		if err := scope.StageCandidates(ctx, replaced); err != nil {
			return fmt.Errorf("stage deleted game assets: %w", err)
		}
		result.Version = request.ExpectedVersion + 1
		return nil
	})
	if err != nil {
		return DeleteResult{}, fmt.Errorf("delete game asset: %w", err)
	}
	return result, nil
}
