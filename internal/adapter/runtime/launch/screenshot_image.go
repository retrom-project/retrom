package launch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/files/mediaasset"
	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/launch"
)

type screenshotImages struct{ blobs *blobstore.Store }

func (images screenshotImages) Read(ctx context.Context, reader io.Reader) (application.ScreenshotImage, error) {
	if images.blobs == nil || reader == nil {
		return application.ScreenshotImage{}, ErrReviewScreenshotInvalid
	}
	if err := ctx.Err(); err != nil {
		return application.ScreenshotImage{}, fmt.Errorf("read screenshot bytes: %w", err)
	}
	candidate, err := images.blobs.Stage(
		io.LimitReader(screenshotContextReader{context: ctx, source: reader}, mediaasset.MaxImageBytes+1),
	)
	if err != nil {
		return application.ScreenshotImage{}, fmt.Errorf("stage screenshot: %w", err)
	}
	defer func() { cleanup.Error("discard screenshot candidate", candidate.Discard()) }()
	metadata := candidate.Metadata()
	image, err := inspectScreenshotFile(metadata)
	if err != nil {
		return application.ScreenshotImage{}, err
	}
	if err := ctx.Err(); err != nil {
		return application.ScreenshotImage{}, fmt.Errorf("finish screenshot read: %w", err)
	}
	metadata, err = candidate.Commit()
	if err != nil {
		return application.ScreenshotImage{}, fmt.Errorf("publish screenshot bytes: %w", err)
	}
	return application.ScreenshotImage{
		SHA256: metadata.SHA256, MD5: metadata.MD5, SHA1: metadata.SHA1, CRC32: metadata.CRC32,
		SizeBytes: metadata.Size, MediaType: image.MediaType, WidthPX: image.WidthPX, HeightPX: image.HeightPX,
	}, nil
}

func inspectScreenshotFile(metadata blobstore.Metadata) (mediaasset.Image, error) {
	if metadata.Size < 1 || metadata.Size > mediaasset.MaxImageBytes {
		return mediaasset.Image{}, ErrReviewScreenshotInvalid
	}
	file, err := os.Open(metadata.Path)
	if err != nil {
		return mediaasset.Image{}, fmt.Errorf("open screenshot candidate: %w", err)
	}
	defer func() { cleanup.Error("close screenshot candidate", file.Close()) }()
	image, err := mediaasset.InspectImage(file, metadata.Size)
	if err != nil || image.MediaType != "image/png" && image.MediaType != "image/jpeg" {
		return mediaasset.Image{}, ErrReviewScreenshotInvalid
	}
	return image, nil
}

type screenshotContextReader struct {
	context context.Context
	source  io.Reader
}

func (reader screenshotContextReader) Read(output []byte) (int, error) {
	if err := reader.context.Err(); err != nil {
		return 0, fmt.Errorf("read screenshot stream: %w", err)
	}
	count, err := reader.source.Read(output)
	if cause := reader.context.Err(); cause != nil {
		return count, fmt.Errorf("read screenshot stream: %w", cause)
	}
	if errors.Is(err, io.EOF) {
		return count, io.EOF
	}
	if err != nil {
		return count, fmt.Errorf("read screenshot stream: %w", err)
	}
	return count, nil
}
