package launch

import (
	"context"
	"errors"
	"io"
	"time"
)

var ErrReviewScreenshotInvalid = errors.New("REVIEW_SCREENSHOT_INVALID")

type ReviewScreenshot struct {
	ID, ImportItemID, ValidationID  string
	ProviderID, TargetID            string
	WidthPX, HeightPX, CapturedAtMS int64
}

type ScreenshotSource struct {
	PreviewID, ItemID, SourceSnapshotID, PlatformInstanceID string
	ValidationID, ProviderID, TargetID                      string
	CredentialHash                                          []byte
	State                                                   string
	HardExpiresAtMS                                         int64
}

type ScreenshotImage struct {
	SHA256, MD5, SHA1, CRC32, MediaType string
	SizeBytes, WidthPX, HeightPX        int64
}

type ScreenshotWrite struct {
	ID     string
	Source ScreenshotSource
	Image  ScreenshotImage
	AtMS   int64
}

type ScreenshotImages interface {
	Read(context.Context, io.Reader) (ScreenshotImage, error)
}

type ScreenshotScope interface {
	Current(context.Context, string) (ScreenshotSource, bool, error)
	Replace(context.Context, ScreenshotWrite) error
}

type ScreenshotRepository interface {
	Preview(context.Context, string) (ScreenshotSource, bool, error)
	WithScreenshot(context.Context, func(ScreenshotScope) error) error
}

type ScreenshotEnvironment struct {
	Now     func() time.Time
	Matches MatchCapability
	NewID   func() (string, error)
}
