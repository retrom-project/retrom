package launch

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type failingScreenshotRepository struct{ cause error }

func (repository failingScreenshotRepository) Preview(context.Context, string) (ScreenshotSource, bool, error) {
	return ScreenshotSource{}, false, repository.cause
}

func (failingScreenshotRepository) WithScreenshot(context.Context, func(ScreenshotScope) error) error {
	panic("writer must not open after read failure")
}

type unreadScreenshotImages struct{}

func (unreadScreenshotImages) Read(context.Context, io.Reader) (ScreenshotImage, error) {
	panic("image must not be read after authorization failure")
}

func TestScreenshotSaverPreservesAuthorizationCause(t *testing.T) {
	cause := errors.New("screenshot repository unavailable")
	service := NewScreenshotSaver(failingScreenshotRepository{cause}, unreadScreenshotImages{}, ScreenshotEnvironment{})
	result, err := service.Store(t.Context(), "preview", "capability", strings.NewReader("image"))
	if !errors.Is(err, cause) || result.ID != "" {
		t.Fatalf("screenshot=%q error=%v", result.ID, err)
	}
}
