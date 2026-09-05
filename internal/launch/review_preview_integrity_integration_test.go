//go:build integration

package launch

import "testing"

func TestReviewPreviewCreationRechecksItsOwnerInTheWriteTransaction(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	source, err := fixture.launcher.reviewPreviewSource(t.Context(), fixture.itemID)
	if err != nil {
		t.Fatal(err)
	}
	content, err := fixture.launcher.reviewPreviewContent(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	mustRPGLaunchSQL(t, fixture.database, `
UPDATE import_items SET state='DISCARDED',completed_at_ms=?,updated_at_ms=?,version=version+1 WHERE id=?`,
		fixture.now.UnixMilli(), fixture.now.UnixMilli(), fixture.itemID)
	err = fixture.launcher.persistReviewPreview(t.Context(), ReviewPreviewRequest{
		ImportItemID: fixture.itemID, ActorUserID: "reviewer", IdempotencyKey: "stale-owner",
	}, source, content, "stale-preview", make([]byte, 32))
	if err == nil {
		t.Fatal("a source read before review termination still created a new preview")
	}
}

func TestTemporaryReviewPayloadCannotBeRewrittenAfterCloseOrReboundForRestore(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	preview := fixture.preview(t, "source")
	if _, _, err := fixture.saver.CreateManual(t.Context(), preview.PreviewID, preview.Capability,
		"state", reviewCheckpointRequest(t, "checkpoint")); err != nil {
		t.Fatal(err)
	}
	restore, err := fixture.launcher.CreateReviewPreview(t.Context(), ReviewPreviewRequest{
		ImportItemID: fixture.itemID, ActorUserID: "reviewer", IdempotencyKey: "restore",
		RestoreFromPreviewID: &preview.PreviewID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.ExecContext(t.Context(), `
UPDATE review_preview_sessions SET restore_payload_blob_id='rpg-project-a' WHERE id=?`, restore.PreviewID); err == nil {
		t.Fatal("restore snapshot accepted a payload replacement")
	}
	if _, err := fixture.launcher.RecordPlay(t.Context(), preview.PreviewID, preview.Capability, "finish",
		PlayEvent{ClientSequence: 0, ClientObservedAtMS: fixture.now.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.ExecContext(t.Context(), `
UPDATE review_preview_sessions SET checkpoint_payload_blob_id='rpg-project-a' WHERE id=?`, preview.PreviewID); err == nil {
		t.Fatal("closed review accepted a checkpoint write")
	}
	if _, err := fixture.database.ExecContext(t.Context(), `
UPDATE review_preview_sessions SET restore_from_preview_id=? WHERE id=?`, preview.PreviewID, preview.PreviewID); err == nil {
		t.Fatal("a running/closed preview acquired a new restore source")
	}
}
