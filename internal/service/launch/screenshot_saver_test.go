package launch

import (
	"context"
	"errors"
	"io"
	model "retrom/internal/model/launch"
	"strings"
	"testing"
)

type failingScreenshotRepository struct{ cause error }

func (repository failingScreenshotRepository) Preview(context.Context, string) (model.ScreenshotSource, bool, error) {
	return model.ScreenshotSource{}, false, repository.cause
}

func (failingScreenshotRepository) WithScreenshot(context.Context, func(model.ScreenshotScope) error) error {
	panic("writer must not open after read failure")
}

type unreadScreenshotImages struct{}

func (unreadScreenshotImages) Read(context.Context, io.Reader) (model.ScreenshotImage, error) {
	panic("image must not be read after authorization failure")
}

func TestScreenshotSaverPreservesAuthorizationCause(t *testing.T) {
	cause := errors.New("screenshot repository unavailable")
	service := NewScreenshotSaver(failingScreenshotRepository{cause}, unreadScreenshotImages{}, model.ScreenshotEnvironment{})
	result, err := service.Store(t.Context(), "preview", "capability", strings.NewReader("image"))
	if !errors.Is(err, cause) || result.ID != "" {
		t.Fatalf("screenshot=%q error=%v", result.ID, err)
	}
}
