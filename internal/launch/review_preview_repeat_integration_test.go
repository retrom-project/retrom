//go:build integration

package launch

import (
	"bytes"
	"database/sql"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/libraryimport"
)

func assertRepeatedPreviewKeepsScreenshot(
	t *testing.T, database *sql.DB, service *Service, importer *libraryimport.Service,
	actorID string, screenshot ReviewScreenshot, image []byte,
) {
	t.Helper()
	ctx := t.Context()
	readEvidence := func() (string, string, int64, int) {
		t.Helper()
		var validationID, screenshotID string
		var version int64
		var count int
		if err := database.QueryRowContext(ctx, `
SELECT validation.id,COALESCE(screenshot.id,''),draft.version,
 (SELECT count(*) FROM import_item_core_validations WHERE import_item_id=draft.import_item_id)
FROM review_drafts draft
JOIN import_item_core_validations validation ON validation.id=(
 SELECT id FROM import_item_core_validations WHERE import_item_id=draft.import_item_id
 ORDER BY created_at_ms DESC,id DESC LIMIT 1)
LEFT JOIN review_runtime_screenshots screenshot ON screenshot.import_item_id=draft.import_item_id
 AND screenshot.validation_id=validation.id
WHERE draft.import_item_id=?
`, screenshot.ImportItemID).Scan(&validationID, &screenshotID, &version, &count); err != nil {
			t.Fatal(err)
		}
		return validationID, screenshotID, version, count
	}
	validationID, screenshotID, version, count := readEvidence()
	if screenshotID != screenshot.ID {
		t.Fatalf("initial screenshot=%s, want %s", screenshotID, screenshot.ID)
	}
	var next ReviewPreviewCreated
	for index := range 2 {
		if err := importer.RefreshReviewPreviewValidation(ctx, screenshot.ImportItemID); err != nil {
			t.Fatal(err)
		}
		created, err := service.CreateReviewPreview(ctx, ReviewPreviewRequest{
			ImportItemID: screenshot.ImportItemID, ActorUserID: actorID,
			IdempotencyKey:     fmt.Sprintf("repeat-%s-%d", screenshot.ID, index),
			ClientCapabilities: Capabilities{SecureContext: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.ReviewPreviewConfig(ctx, created.PreviewID, created.Capability); err != nil {
			t.Fatal(err)
		}
		next = created
		currentValidation, currentScreenshot, currentVersion, currentCount := readEvidence()
		if currentValidation != validationID || currentScreenshot != screenshotID || currentCount != count {
			t.Fatalf("repeated trial changed review evidence: validation=%s screenshot=%s version=%d count=%d; want %s/%s/%d/%d",
				currentValidation, currentScreenshot, currentVersion, currentCount, validationID, screenshotID, version, count)
		}
		if index > 0 && currentVersion != version {
			t.Fatal("unchanged repeated preview changed the draft version")
		}
		version = currentVersion
	}
	updated, err := service.StoreReviewScreenshot(ctx, next.PreviewID, next.Capability, bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	currentValidation, currentScreenshot, _, _ := readEvidence()
	if updated.ID == screenshotID || currentScreenshot != updated.ID || currentValidation != validationID {
		t.Fatal("a new capture did not replace the screenshot for the same validation")
	}
	var retained int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM review_runtime_screenshots WHERE import_item_id=?`, screenshot.ImportItemID).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != 1 {
		t.Fatalf("saving a screenshot retained %d screenshots; want one current result", retained)
	}
}

func seedOlderReviewScreenshot(t *testing.T, database *sql.DB, screenshot ReviewScreenshot) {
	t.Helper()
	ctx := t.Context()
	validationID, screenshotID := uuid.NewString(), uuid.NewString()
	if _, err := database.ExecContext(ctx, `
INSERT INTO import_item_core_validations(id,import_item_id,target_platform_instance_id,platform_instance_version,
core_id,provider_id,target_id,dat_version_id,default_dos_entry,source_manifest_digest,source_snapshot_id,
prepublish_input_digest,status,compatibility_code,dependency_snapshot_json,created_at_ms)
SELECT ?,import_item_id,target_platform_instance_id,platform_instance_version,core_id,provider_id,target_id,
dat_version_id,default_dos_entry,source_manifest_digest,source_snapshot_id,prepublish_input_digest,
status,compatibility_code,dependency_snapshot_json,0
FROM import_item_core_validations WHERE id=?
`, validationID, screenshot.ValidationID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO review_runtime_screenshots(id,import_item_id,preview_session_id,source_snapshot_id,
validation_id,provider_id,target_id,blob_id,media_type,width_px,height_px,captured_at_ms,created_at_ms,updated_at_ms)
SELECT ?,import_item_id,preview_session_id,source_snapshot_id,?,provider_id,target_id,blob_id,
media_type,width_px,height_px,captured_at_ms,created_at_ms,updated_at_ms
FROM review_runtime_screenshots WHERE id=?
`, screenshotID, validationID, screenshot.ID); err != nil {
		t.Fatal(err)
	}
}
