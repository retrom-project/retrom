package launch

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"time"

	"retrom/internal/adapter/files/mediaasset"
)

type ScreenshotSaver struct {
	repository  ScreenshotRepository
	images      ScreenshotImages
	environment ScreenshotEnvironment
}

func NewScreenshotSaver(
	repository ScreenshotRepository,
	images ScreenshotImages,
	environment ScreenshotEnvironment,
) *ScreenshotSaver {
	if environment.NewID == nil {
		environment.NewID = newProductID
	}
	if environment.Now == nil {
		environment.Now = time.Now
	}
	return &ScreenshotSaver{repository: repository, images: images, environment: environment}
}

func (service *ScreenshotSaver) Store(
	ctx context.Context,
	previewID, capability string,
	reader io.Reader,
) (ReviewScreenshot, error) {
	if service.images == nil {
		return ReviewScreenshot{}, ErrReviewScreenshotInvalid
	}
	before, found, err := service.repository.Preview(ctx, previewID)
	if err != nil {
		return ReviewScreenshot{}, fmt.Errorf("read screenshot credential: %w", err)
	}
	if !found || !service.authorized(before, capability, service.environment.Now().UnixMilli()) {
		return ReviewScreenshot{}, ErrCredential
	}
	image, err := service.images.Read(ctx, reader)
	if err != nil {
		return ReviewScreenshot{}, fmt.Errorf("read screenshot image: %w", err)
	}
	if !validScreenshotImage(image) {
		return ReviewScreenshot{}, ErrReviewScreenshotInvalid
	}
	var result ReviewScreenshot
	err = service.repository.WithScreenshot(ctx, func(scope ScreenshotScope) error {
		var writeErr error
		result, writeErr = service.capture(ctx, scope, before, capability, image)
		return writeErr
	})
	if err != nil {
		return ReviewScreenshot{}, fmt.Errorf("capture review screenshot: %w", err)
	}
	return result, nil
}

func (service *ScreenshotSaver) capture(
	ctx context.Context,
	scope ScreenshotScope,
	before ScreenshotSource,
	capability string,
	image ScreenshotImage,
) (ReviewScreenshot, error) {
	current, found, err := scope.Current(ctx, before.PreviewID)
	if err != nil {
		return ReviewScreenshot{}, fmt.Errorf("read final screenshot source: %w", err)
	}
	now := service.environment.Now().UnixMilli()
	if !found || !service.authorized(current, capability, now) || !reflect.DeepEqual(before, current) {
		return ReviewScreenshot{}, ErrCredential
	}
	id, err := checkedProductID(service.environment.NewID)
	if err != nil {
		return ReviewScreenshot{}, fmt.Errorf("create screenshot identity: %w", err)
	}
	if err := scope.Replace(ctx, ScreenshotWrite{ID: id, Source: current, Image: image, AtMS: now}); err != nil {
		return ReviewScreenshot{}, fmt.Errorf("replace current screenshot: %w", err)
	}
	return ReviewScreenshot{
		ID: id, ImportItemID: current.ItemID, ValidationID: current.ValidationID,
		ProviderID: current.ProviderID, TargetID: current.TargetID,
		WidthPX: image.WidthPX, HeightPX: image.HeightPX, CapturedAtMS: now,
	}, nil
}

func (service *ScreenshotSaver) authorized(source ScreenshotSource, capability string, now int64) bool {
	return source.PreviewID != "" && source.State == "ACTIVE" && now >= 0 && source.HardExpiresAtMS > now &&
		service.environment.Matches != nil && service.environment.Matches(capability, source.CredentialHash)
}

func validScreenshotImage(image ScreenshotImage) bool {
	return image.SizeBytes > 0 && image.SizeBytes <= mediaasset.MaxImageBytes && image.WidthPX > 0 && image.HeightPX > 0 &&
		image.WidthPX <= mediaasset.MaxImagePixels/image.HeightPX &&
		(image.MediaType == "image/png" || image.MediaType == "image/jpeg")
}
