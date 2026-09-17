package gameassets

import (
	"context"
	"fmt"

	"retrom/internal/adapter/files/mediaasset"
	modelGameAssets "retrom/internal/model/gameassets"
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
	err = service.repository.CommitCreate(ctx, modelGameAssets.CreateCommand{
		GameID: request.GameID, UploadFileID: request.UploadFileID,
		ExpectedVersion: request.ExpectedVersion, NowMS: request.NowMS,
		Kind: request.Kind, Ordinal: request.Ordinal,
		AssetID: assetID, ConsumptionID: consumptionID,
		Asset: modelGameAssets.PreparedAsset{
			UploadID: request.Asset.UploadID, BlobID: request.Asset.BlobID,
			Digest: request.Asset.Digest, SizeBytes: request.Asset.SizeBytes,
			MediaType: request.Asset.MediaType,
			WidthPX:   request.Asset.WidthPX, HeightPX: request.Asset.HeightPX,
		},
	})
	if err != nil {
		return CreateResult{}, fmt.Errorf("create game asset: %w", err)
	}
	return result, nil
}

// Delete removes the video slot and stages its old payload atomically.
func (service *Service) Delete(ctx context.Context, request DeleteRequest) (DeleteResult, error) {
	if request.GameID == "" || request.Kind != "VIDEO" || request.ExpectedVersion < 1 {
		return DeleteResult{}, ErrInvalid
	}
	modelResult, err := service.repository.CommitDelete(
		ctx, modelGameAssets.DeleteCommand{
			GameID: request.GameID, Kind: request.Kind,
			ExpectedVersion: request.ExpectedVersion, NowMS: request.NowMS,
		},
	)
	if err != nil {
		return DeleteResult{}, fmt.Errorf("delete game asset: %w", err)
	}
	return DeleteResult{Version: modelResult.Version}, nil
}
