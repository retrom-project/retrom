package launch

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"time"

	model "retrom/internal/model/launch"

	"retrom/internal/adapter/files/mediaasset"
)

type ScreenshotSaver struct {
	repository  model.ScreenshotRepository
	images      model.ScreenshotImages
	environment model.ScreenshotEnvironment
}

func NewScreenshotSaver(
	repository model.ScreenshotRepository,
	images model.ScreenshotImages,
	environment model.ScreenshotEnvironment,
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
) (model.ReviewScreenshot, error) {
	if service.images == nil {
		return model.ReviewScreenshot{}, model.ErrReviewScreenshotInvalid
	}
	before, found, err := service.repository.Preview(ctx, previewID)
	if err != nil {
		return model.ReviewScreenshot{}, fmt.Errorf("read screenshot credential: %w", err)
	}
	if !found || !service.authorized(before, capability, service.environment.Now().UnixMilli()) {
		return model.ReviewScreenshot{}, model.ErrCredential
	}
	image, err := service.images.Read(ctx, reader)
	if err != nil {
		return model.ReviewScreenshot{}, fmt.Errorf("read screenshot image: %w", err)
	}
	if !validScreenshotImage(image) {
		return model.ReviewScreenshot{}, model.ErrReviewScreenshotInvalid
	}
	result, err := service.capture(ctx, before, capability, image)
	if err != nil {
		return model.ReviewScreenshot{}, fmt.Errorf("capture review screenshot: %w", err)
	}
	return result, nil
}

func (service *ScreenshotSaver) capture(
	ctx context.Context,
	before model.ScreenshotSource,
	capability string,
	image model.ScreenshotImage,
) (model.ReviewScreenshot, error) {
	current, found, err := service.repository.LoadScreenshotSource(ctx, before.PreviewID)
	if err != nil {
		return model.ReviewScreenshot{}, fmt.Errorf("read final screenshot source: %w", err)
	}
	now := service.environment.Now().UnixMilli()
	if !found || !service.authorized(current, capability, now) || !reflect.DeepEqual(before, current) {
		return model.ReviewScreenshot{}, model.ErrCredential
	}
	id, err := checkedProductID(service.environment.NewID)
	if err != nil {
		return model.ReviewScreenshot{}, fmt.Errorf("create screenshot identity: %w", err)
	}
	write := model.ScreenshotWrite{ID: id, Source: current, Image: image, AtMS: now}
	if err := service.repository.CommitScreenshot(ctx, write); err != nil {
		return model.ReviewScreenshot{}, fmt.Errorf("replace current screenshot: %w", err)
	}
	return model.ReviewScreenshot{
		ID: id, ImportItemID: current.ItemID, ValidationID: current.ValidationID,
		ProviderID: current.ProviderID, TargetID: current.TargetID,
		WidthPX: image.WidthPX, HeightPX: image.HeightPX, CapturedAtMS: now,
	}, nil
}

func (service *ScreenshotSaver) authorized(source model.ScreenshotSource, capability string, now int64) bool {
	return source.PreviewID != "" && source.State == "ACTIVE" && now >= 0 && source.HardExpiresAtMS > now &&
		service.environment.Matches != nil && service.environment.Matches(capability, source.CredentialHash)
}

func validScreenshotImage(image model.ScreenshotImage) bool {
	return image.SizeBytes > 0 && image.SizeBytes <= mediaasset.MaxImageBytes && image.WidthPX > 0 && image.HeightPX > 0 &&
		image.WidthPX <= mediaasset.MaxImagePixels/image.HeightPX &&
		(image.MediaType == "image/png" || image.MediaType == "image/jpeg")
}
