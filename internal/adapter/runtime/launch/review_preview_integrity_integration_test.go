//go:build integration

package launch

import (
	"testing"
	"time"

	"retrom/internal/persistence/recordstore"
)

func TestReviewPreviewCreationRechecksItsOwnerInTheWriteTransaction(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	originalClock := fixture.launcher.now
	interleaved := false
	fixture.launcher.now = func() time.Time {
		if !interleaved {
			interleaved = true
			mustRPGLaunchSQL(t, fixture.database, `UPDATE import_items SET state='DISCARDED',completed_at_ms=?,updated_at_ms=?,version=version+1 WHERE id=?`, fixture.now.UnixMilli(), fixture.now.UnixMilli(), fixture.itemID)
		}
		return originalClock()
	}
	created, err := fixture.launcher.CreateReviewPreview(t.Context(), ReviewPreviewRequest{
		ImportItemID: fixture.itemID, ActorUserID: "reviewer", IdempotencyKey: "stale-owner",
	})
	if err == nil || created.PreviewID != "" || !interleaved {
		t.Fatalf("source read before review termination created a preview: id=%q error=%v", created.PreviewID, err)
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
	if _, err := recordstore.UpdateReviewPreviewSessions(t.Context(), fixture.database, recordstore.Update{
		Set: `restore_payload_blob_id='rpg-project-a'`,
		Scope: recordstore.Scope{
			Where: `id=?`,
			Args:  []any{restore.PreviewID},
		},
	}); err == nil {
		t.Fatal("restore snapshot accepted a payload replacement")
	}
	if _, err := fixture.launcher.RecordPlay(t.Context(), preview.PreviewID, preview.Capability, "finish",
		PlayEvent{ClientSequence: 0, ClientObservedAtMS: fixture.now.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if _, err := recordstore.UpdateReviewPreviewSessions(t.Context(), fixture.database, recordstore.Update{
		Set: `checkpoint_payload_blob_id='rpg-project-a'`,
		Scope: recordstore.Scope{
			Where: `id=?`,
			Args:  []any{preview.PreviewID},
		},
	}); err == nil {
		t.Fatal("closed review accepted a checkpoint write")
	}
	if _, err := recordstore.UpdateReviewPreviewSessions(t.Context(), fixture.database, recordstore.Update{
		Set: `restore_from_preview_id=?`,
		Scope: recordstore.Scope{
			Where: `id=?`,
			Args:  []any{preview.PreviewID},
		},
		Values: []any{preview.PreviewID},
	}); err == nil {
		t.Fatal("a running/closed preview acquired a new restore source")
	}
}
