package libraryimport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"retrom/internal/hasheous"
	"retrom/internal/mediaasset"
)

type preparedReviewAsset struct {
	MediaType     string
	Width, Height *int
}

func (service *ReviewAssetUploads) prepare(
	ctx context.Context, source ReviewAssetSource, kind string,
) (preparedReviewAsset, error) {
	if err := ctx.Err(); err != nil {
		return preparedReviewAsset{}, fmt.Errorf("prepare review asset: %w", err)
	}
	if kind == "VIDEO" && (source.SizeBytes < 1 || source.SizeBytes > mediaasset.MaxVideoBytes) {
		return preparedReviewAsset{}, ErrReviewAssetVideoInvalid
	}
	file, err := service.blobs.OpenRecord(source.FileRecord)
	if err != nil {
		return preparedReviewAsset{}, fmt.Errorf("%w: open: %w", ErrReviewAssetStorageUnavailable, err)
	}
	limit := int64(mediaasset.MaxImageBytes + 1)
	if kind == "VIDEO" {
		limit = 4096
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, limit))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return preparedReviewAsset{}, fmt.Errorf("%w: read: %w", ErrReviewAssetStorageUnavailable, err)
	}
	if err := ctx.Err(); err != nil {
		return preparedReviewAsset{}, fmt.Errorf("prepare review asset: %w", err)
	}
	if kind == "VIDEO" {
		mediaType, err := mediaasset.InspectVideo(bytes.NewReader(contents), source.SizeBytes)
		if err != nil {
			return preparedReviewAsset{}, fmt.Errorf("%w: %w", ErrReviewAssetVideoInvalid, err)
		}
		return preparedReviewAsset{MediaType: mediaType}, nil
	}
	image, err := hasheous.ValidateImage(contents, "")
	if err != nil {
		return preparedReviewAsset{}, fmt.Errorf("%w: %w", ErrReviewAssetImageInvalid, err)
	}
	return preparedReviewAsset{MediaType: image.MediaType, Width: &image.Width, Height: &image.Height}, nil
}
