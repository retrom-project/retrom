package launch

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	model "retrom/internal/model/launch"
)

type failingScreenshotRepository struct{ cause error }

func (repository failingScreenshotRepository) Preview(context.Context, string) (model.ScreenshotSource, bool, error) {
	return model.ScreenshotSource{}, false, repository.cause
}

func (failingScreenshotRepository) LoadScreenshotSource(context.Context, string) (model.ScreenshotSource, bool, error) {
	panic("source must not load after read failure")
}

func (failingScreenshotRepository) CommitScreenshot(context.Context, model.ScreenshotWrite) error {
	panic("writer must not open after read failure")
}

type unreadScreenshotImages struct{}

func (unreadScreenshotImages) Read(context.Context, io.Reader) (model.ScreenshotImage, error) {
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
