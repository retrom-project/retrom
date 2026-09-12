//go:build integration

package launch

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/png"
	"io"
	"testing"
	"time"

	"modernc.org/sqlite"

	"retrom/internal/blobstore"
)

func screenshotFixture(t *testing.T) (reviewCheckpointFixture, ReviewPreviewCreated, []byte) {
	t.Helper()
	fixture := newReviewCheckpointFixture(t)
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture.launcher.WithBlobStore(blobs)
	preview := fixture.preview(t, "screenshot-regression")
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return fixture, preview, imageBytes.Bytes()
}

func screenshotCounts(t *testing.T, database *sql.DB) (int, int) {
	t.Helper()
	var screenshots, blobs int
	if err := database.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM review_runtime_screenshots),(SELECT count(*) FROM blobs)`).Scan(&screenshots, &blobs); err != nil {
		t.Fatal(err)
	}
	return screenshots, blobs
}

type screenshotFaultReader struct{ cause error }

func (reader screenshotFaultReader) Read([]byte) (int, error) { return 0, reader.cause }

type screenshotHookReader struct {
	reader io.Reader
	before func()
}

func (reader *screenshotHookReader) Read(output []byte) (int, error) {
	if reader.before != nil {
		before := reader.before
		reader.before = nil
		before()
	}
	return reader.reader.Read(output)
}

func TestScreenshotPreservesCancelledContextBeforeReadingBody(t *testing.T) {
	fixture, preview, _ := screenshotFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	read := false
	reader := &screenshotHookReader{reader: bytes.NewReader(nil), before: func() { read = true }}
	result, err := fixture.launcher.StoreReviewScreenshot(ctx, preview.PreviewID, preview.Capability, reader)
	if !errors.Is(err, context.Canceled) || result.ID != "" || read {
		t.Fatalf("cancelled screenshot id=%q read=%v error=%v", result.ID, read, err)
	}
}

func TestScreenshotPreservesFinalAuthoritySQLCause(t *testing.T) {
	fixture, preview, contents := screenshotFixture(t)
	beforeShots, beforeBlobs := screenshotCounts(t, fixture.database)
	mustRPGLaunchSQL(t, fixture.database, `ALTER TABLE review_drafts RENAME TO unavailable_screenshot_drafts`)
	result, err := fixture.launcher.StoreReviewScreenshot(t.Context(), preview.PreviewID, preview.Capability, bytes.NewReader(contents))
	var storage *sqlite.Error
	if !errors.As(err, &storage) || result.ID != "" {
		t.Fatalf("authority SQL id=%q error=%v", result.ID, err)
	}
	shots, blobs := screenshotCounts(t, fixture.database)
	if shots != beforeShots || blobs != beforeBlobs {
		t.Fatal("authority failure wrote screenshot ownership")
	}
}

func TestScreenshotPreservesReaderCause(t *testing.T) {
	fixture, preview, _ := screenshotFixture(t)
	cause := errors.New("screenshot upload interrupted")
	result, err := fixture.launcher.StoreReviewScreenshot(t.Context(), preview.PreviewID, preview.Capability, screenshotFaultReader{cause: cause})
	if !errors.Is(err, cause) || result.ID != "" {
		t.Fatalf("reader failure id=%q error=%v", result.ID, err)
	}
}

func TestScreenshotRechecksDirectoryAfterImageRead(t *testing.T) {
	fixture, preview, contents := screenshotFixture(t)
	beforeShots, beforeBlobs := screenshotCounts(t, fixture.database)
	reader := &screenshotHookReader{reader: bytes.NewReader(contents), before: func() {
		mustRPGLaunchSQL(t, fixture.database, `UPDATE platform_instances SET enabled=0 WHERE id=(SELECT target_platform_instance_id FROM review_preview_sessions WHERE id=?)`, preview.PreviewID)
	}}
	result, err := fixture.launcher.StoreReviewScreenshot(t.Context(), preview.PreviewID, preview.Capability, reader)
	if !errors.Is(err, ErrCredential) || result.ID != "" {
		t.Fatalf("disabled directory id=%q error=%v", result.ID, err)
	}
	shots, blobs := screenshotCounts(t, fixture.database)
	if shots != beforeShots || blobs != beforeBlobs {
		t.Fatal("disabled directory retained screenshot ownership")
	}
}

func TestScreenshotCaptureTimestampCannotExceedAuthorizedLifetime(t *testing.T) {
	fixture, preview, contents := screenshotFixture(t)
	var hardEnd int64
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT hard_expires_at_ms FROM review_preview_sessions WHERE id=?`, preview.PreviewID).Scan(&hardEnd); err != nil {
		t.Fatal(err)
	}
	calls := 0
	fixture.launcher.now = func() time.Time {
		calls++
		if calls >= 3 {
			return time.UnixMilli(hardEnd)
		}
		return *fixture.now
	}
	result, err := fixture.launcher.StoreReviewScreenshot(t.Context(), preview.PreviewID, preview.Capability, bytes.NewReader(contents))
	if err != nil && !errors.Is(err, ErrCredential) {
		t.Fatal(err)
	}
	if err == nil && (result.ID == "" || result.CapturedAtMS >= hardEnd) {
		t.Fatalf("capture=%d expires=%d id=%q", result.CapturedAtMS, hardEnd, result.ID)
	}
}
