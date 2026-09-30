//go:build integration

package launch

import (
	"bytes"
	"fmt"
	"testing"

	reviewservice "retrom/internal/service/libraryimport"

	dbapi "retrom/internal/database"
	"retrom/internal/libraryimport"
)

func assertRepeatedPreviewKeepsScreenshot(
	t *testing.T, database dbapi.DB, service *Service, importer *libraryimport.Service,
	actorID string, screenshot reviewservice.ReviewScreenshot, image []byte,
) {
	t.Helper()
	ctx := t.Context()
	readEvidence := func() (string, int64) {
		t.Helper()
		var screenshotID string
		var version int64
		if err := dbapi.QueryRowContext(ctx, database, `
SELECT COALESCE(screenshot.id,''),draft.review_version
FROM import_items draft
LEFT JOIN review_runtime_screenshots screenshot ON screenshot.import_item_id=draft.id
WHERE draft.id=?
`, screenshot.ImportItemID).Scan(&screenshotID, &version); err != nil {
			t.Fatal(err)
		}
		return screenshotID, version
	}
	screenshotID, version := readEvidence()
	if screenshotID != screenshot.ID {
		t.Fatalf("initial screenshot=%s, want %s", screenshotID, screenshot.ID)
	}
	var next ReviewPreviewCreated
	for index := range 2 {
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
		currentScreenshot, currentVersion := readEvidence()
		if currentScreenshot != screenshotID {
			t.Fatalf("repeated trial changed item screenshot: %s; want %s", currentScreenshot, screenshotID)
		}
		if currentVersion != version {
			t.Fatal("unchanged repeated preview changed the draft version")
		}
	}
	updated, err := service.StoreReviewScreenshot(ctx, next.PreviewID, next.Capability, bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	currentScreenshot, _ := readEvidence()
	if updated.ID == screenshotID || currentScreenshot != updated.ID {
		t.Fatal("a new capture did not replace the screenshot for the same item")
	}
	var retained int
	if err := dbapi.QueryRowContext(ctx, database, `SELECT count(*) FROM review_runtime_screenshots WHERE import_item_id=?`, screenshot.ImportItemID).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != 1 {
		t.Fatalf("saving a screenshot retained %d screenshots; want one current result", retained)
	}
}

func seedOlderReviewScreenshot(t *testing.T, database dbapi.DB, screenshot reviewservice.ReviewScreenshot) {
	t.Helper()
	if _, err := database.ExecContext(t.Context(), `UPDATE review_runtime_screenshots SET captured_at_ms=0 WHERE import_item_id=?`, screenshot.ImportItemID); err != nil {
		t.Fatal(err)
	}
}
