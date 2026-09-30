//go:build integration

package launch

import (
	"testing"
	"time"

	dbapi "retrom/internal/database"
)

func TestRestoredPreviewKeepsCheckpointAfterOriginalPreviewExpires(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	original := fixture.preview(t, "original-expiry")
	if _, _, err := fixture.saver.CreateManual(t.Context(), original.PreviewID, original.Capability, "checkpoint", reviewCheckpointRequest(t, "point-B")); err != nil {
		t.Fatal(err)
	}
	*fixture.now = fixture.now.Add(time.Hour)
	restored, err := fixture.launcher.CreateReviewPreview(t.Context(), ReviewPreviewRequest{
		ImportItemID: fixture.itemID, ActorUserID: "reviewer", IdempotencyKey: "restore-after-hour", RestoreFromPreviewID: &original.PreviewID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.launcher.ReviewPreviewConfig(t.Context(), restored.PreviewID, restored.Capability); err != nil {
		t.Fatal(err)
	}
	*fixture.now = fixture.now.Add(time.Hour + time.Second)
	if err := fixture.releaser.ReconcileDeletion(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		worked, err := fixture.releaser.RunOnce(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			break
		}
	}
	var state string
	if err := dbapi.QueryRowContext(t.Context(), fixture.database, `SELECT state FROM runtime_preview_sessions WHERE id=?`, original.PreviewID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "EXPIRED" {
		t.Fatalf("original preview did not expire: %s", state)
	}
	if _, err := fixture.saver.StateFile(t.Context(), restored.PreviewID, restored.Capability); err != nil {
		t.Fatalf("active restored preview lost its frozen checkpoint with original expiry: %v", err)
	}
}
