//go:build integration

package launch

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/png"
	"reflect"
	"strings"
	"testing"
	"time"

	"modernc.org/sqlite"

	"retrom/internal/cleanup"
)

type screenshotStored struct {
	ID, PreviewID, ValidationID, BlobID       string
	Width, Height, Captured, Created, Updated int64
}

func screenshotRecords(t *testing.T, database *sql.DB) []screenshotStored {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), `SELECT id,preview_session_id,validation_id,blob_id,
width_px,height_px,captured_at_ms,created_at_ms,updated_at_ms FROM review_runtime_screenshots ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close screenshot rows", rows.Close()) }()
	var records []screenshotStored
	for rows.Next() {
		var record screenshotStored
		if err := rows.Scan(&record.ID, &record.PreviewID, &record.ValidationID, &record.BlobID,
			&record.Width, &record.Height, &record.Captured, &record.Created, &record.Updated); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}

func screenshotPNG(t *testing.T, size int) []byte {
	t.Helper()
	var contents bytes.Buffer
	if err := png.Encode(&contents, image.NewRGBA(image.Rect(0, 0, size, size))); err != nil {
		t.Fatal(err)
	}
	return contents.Bytes()
}

func TestScreenshotReplacementPreservesCreationAndRetiresPriorValidation(t *testing.T) {
	fixture, preview, contents := screenshotFixture(t)
	first, err := fixture.launcher.StoreReviewScreenshot(t.Context(), preview.PreviewID, preview.Capability, bytes.NewReader(contents))
	if err != nil {
		t.Fatal(err)
	}
	before := screenshotRecords(t, fixture.database)
	seedOlderReviewScreenshot(t, fixture.database, first)
	*fixture.now = fixture.now.Add(time.Second)
	second, err := fixture.launcher.StoreReviewScreenshot(t.Context(), preview.PreviewID, preview.Capability, bytes.NewReader(screenshotPNG(t, 3)))
	if err != nil {
		t.Fatal(err)
	}
	after := screenshotRecords(t, fixture.database)
	if len(after) != 1 || second.ID == first.ID || second.ValidationID != first.ValidationID {
		t.Fatalf("first=%+v second=%+v rows=%+v", first, second, after)
	}
	stored := after[0]
	if stored.ID != second.ID || stored.BlobID == before[0].BlobID || stored.Width != 3 || stored.Height != 3 ||
		stored.Created != before[0].Created || stored.Updated != fixture.now.UnixMilli() || stored.Captured != second.CapturedAtMS {
		t.Fatalf("replacement before=%+v after=%+v", before, after)
	}
}

func TestScreenshotLateWriteFailureRollsBackBlobAndPriorScreenshotDeletion(t *testing.T) {
	fixture, preview, contents := screenshotFixture(t)
	first, err := fixture.launcher.StoreReviewScreenshot(t.Context(), preview.PreviewID, preview.Capability, bytes.NewReader(contents))
	if err != nil {
		t.Fatal(err)
	}
	seedOlderReviewScreenshot(t, fixture.database, first)
	before := screenshotRecords(t, fixture.database)
	beforeShots, beforeBlobs := screenshotCounts(t, fixture.database)
	mustRPGLaunchSQL(t, fixture.database, `ALTER TABLE review_runtime_screenshots ADD COLUMN reject_new_width INTEGER NOT NULL DEFAULT 0 CHECK(width_px=2)`)
	result, err := fixture.launcher.StoreReviewScreenshot(t.Context(), preview.PreviewID, preview.Capability, bytes.NewReader(screenshotPNG(t, 3)))
	var storage *sqlite.Error
	if !errors.As(err, &storage) || result != (ReviewScreenshot{}) {
		t.Fatalf("failed replacement result=%+v error=%v", result, err)
	}
	after := screenshotRecords(t, fixture.database)
	afterShots, afterBlobs := screenshotCounts(t, fixture.database)
	if !reflect.DeepEqual(before, after) || afterShots != beforeShots || afterBlobs != beforeBlobs {
		t.Fatalf("rollback before=%+v after=%+v counts=%d/%d want=%d/%d", before, after, afterShots, afterBlobs, beforeShots, beforeBlobs)
	}
}

func TestScreenshotRechecksCurrentReviewEvidenceAfterImageRead(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, reviewCheckpointFixture, ReviewPreviewCreated)
	}{
		{"source", func(t *testing.T, fixture reviewCheckpointFixture, _ ReviewPreviewCreated) {
			mustRPGLaunchSQL(t, fixture.database, `UPDATE review_drafts SET effective_source_snapshot_id=NULL WHERE import_item_id=?`, fixture.itemID)
		}},
		{"review state", func(t *testing.T, fixture reviewCheckpointFixture, _ ReviewPreviewCreated) {
			mustRPGLaunchSQL(t, fixture.database, `UPDATE import_items SET state='DISCARDED' WHERE id=?`, fixture.itemID)
		}},
		{"payload released", func(t *testing.T, fixture reviewCheckpointFixture, _ ReviewPreviewCreated) {
			screenshotReleasePayload(t, fixture)
		}},
		{"directory deleted", func(t *testing.T, fixture reviewCheckpointFixture, preview ReviewPreviewCreated) {
			mustRPGLaunchSQL(t, fixture.database, `UPDATE platform_instances SET deleted_at_ms=? WHERE id=(SELECT target_platform_instance_id FROM review_preview_sessions WHERE id=?)`, fixture.now.UnixMilli(), preview.PreviewID)
		}},
		{"finished", func(t *testing.T, fixture reviewCheckpointFixture, preview ReviewPreviewCreated) {
			mustRPGLaunchSQL(t, fixture.database, `UPDATE review_preview_sessions SET state='FINISHED',finished_at_ms=? WHERE id=?`, fixture.now.UnixMilli(), preview.PreviewID)
		}},
		{"expired", func(t *testing.T, fixture reviewCheckpointFixture, preview ReviewPreviewCreated) {
			var expiry int64
			if err := fixture.database.QueryRowContext(t.Context(), `SELECT hard_expires_at_ms FROM review_preview_sessions WHERE id=?`, preview.PreviewID).Scan(&expiry); err != nil {
				t.Fatal(err)
			}
			*fixture.now = time.UnixMilli(expiry)
		}},
		{"new validation", func(t *testing.T, fixture reviewCheckpointFixture, _ ReviewPreviewCreated) {
			mustRPGLaunchSQL(t, fixture.database, `UPDATE import_item_core_validations SET created_at_ms=? WHERE import_item_id=? AND created_at_ms=0`, fixture.now.UnixMilli()+1, fixture.itemID)
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture, preview, contents := screenshotFixture(t)
			first, err := fixture.launcher.StoreReviewScreenshot(t.Context(), preview.PreviewID, preview.Capability, bytes.NewReader(contents))
			if err != nil {
				t.Fatal(err)
			}
			seedOlderReviewScreenshot(t, fixture.database, first)
			before := screenshotRecords(t, fixture.database)
			beforeShots, beforeBlobs := screenshotCounts(t, fixture.database)
			reader := &screenshotHookReader{reader: bytes.NewReader(screenshotPNG(t, 3)), before: func() { test.change(t, fixture, preview) }}
			result, err := fixture.launcher.StoreReviewScreenshot(t.Context(), preview.PreviewID, preview.Capability, reader)
			if !errors.Is(err, ErrCredential) || result != (ReviewScreenshot{}) {
				t.Fatalf("changed review evidence result=%+v error=%v", result, err)
			}
			afterShots, afterBlobs := screenshotCounts(t, fixture.database)
			if !reflect.DeepEqual(before, screenshotRecords(t, fixture.database)) || afterShots != beforeShots || afterBlobs != beforeBlobs {
				t.Fatal("changed review evidence retained new screenshot ownership")
			}
		})
	}
}

func TestScreenshotPreservesMidReadCancellationWithoutOwnership(t *testing.T) {
	fixture, preview, contents := screenshotFixture(t)
	beforeShots, beforeBlobs := screenshotCounts(t, fixture.database)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader := &screenshotHookReader{reader: bytes.NewReader(contents), before: cancel}
	result, err := fixture.launcher.StoreReviewScreenshot(ctx, preview.PreviewID, preview.Capability, reader)
	if !errors.Is(err, context.Canceled) || result != (ReviewScreenshot{}) {
		t.Fatalf("cancelled screenshot result=%+v error=%v", result, err)
	}
	afterShots, afterBlobs := screenshotCounts(t, fixture.database)
	if afterShots != beforeShots || afterBlobs != beforeBlobs {
		t.Fatal("cancelled image read retained screenshot ownership")
	}
}

func screenshotReleasePayload(t *testing.T, fixture reviewCheckpointFixture) {
	t.Helper()
	mustRPGLaunchSQL(t, fixture.database, `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,
 cancellable,state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
 VALUES('screenshot-release','IMPORT_ITEM',?,'PAYLOAD_RELEASE',?,1,'{}',0,'QUEUED',0,4,0,0,0)`,
		fixture.itemID, strings.Repeat("a", 64))
	mustRPGLaunchSQL(t, fixture.database, `UPDATE import_items SET payload_state='RELEASING',payload_release_job_id='screenshot-release' WHERE id=?`, fixture.itemID)
}
