//go:build integration

package launch

import (
	"context"
	"errors"
	"testing"
	"time"

	persistence "retrom/internal/repo/launch"

	"modernc.org/sqlite"
)

func TestPreviewCreationReplaysAConcurrentCommittedRequest(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	request := ReviewPreviewRequest{ImportItemID: fixture.itemID, ActorUserID: "reviewer", IdempotencyKey: "overlap"}
	competitor := *fixture.launcher
	originalClock := fixture.launcher.now
	interleaved := false
	var winner ReviewPreviewCreated
	fixture.launcher.now = func() time.Time {
		if !interleaved {
			interleaved = true
			var err error
			winner, err = competitor.CreateReviewPreview(t.Context(), request)
			if err != nil {
				t.Fatalf("competing real create: %v", err)
			}
		}
		return originalClock()
	}
	created, err := fixture.launcher.CreateReviewPreview(t.Context(), request)
	if err != nil || !interleaved || created != winner {
		t.Fatalf("same-key request lost committed receipt: created=%q winner=%q error=%v", created.PreviewID, winner.PreviewID, err)
	}
	var count int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM review_preview_sessions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("same-key calls created %d previews", count)
	}
}

func TestPreviewCreationRechecksPlatformEnabledAtCommit(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	var instanceID string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT target_platform_instance_id FROM review_drafts WHERE import_item_id=?`, fixture.itemID).Scan(&instanceID); err != nil {
		t.Fatal(err)
	}
	originalClock := fixture.launcher.now
	interleaved := false
	fixture.launcher.now = func() time.Time {
		if !interleaved {
			interleaved = true
			mustRPGLaunchSQL(t, fixture.database, `UPDATE platform_instances SET enabled=0,version=version+1 WHERE id=?`, instanceID)
		}
		return originalClock()
	}
	created, err := fixture.launcher.CreateReviewPreview(t.Context(), ReviewPreviewRequest{
		ImportItemID: fixture.itemID, ActorUserID: "reviewer", IdempotencyKey: "disabled-platform",
	})
	if err == nil || created.PreviewID != "" || !interleaved {
		t.Fatalf("preview accepted a platform disabled after selection: created=%q error=%v", created.PreviewID, err)
	}
	var count int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM review_preview_sessions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rejected preview left %d rows", count)
	}
}

func TestPreviewCreationSourceQueryPreservesStorageCause(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	mustRPGLaunchSQL(t, fixture.database, `ALTER TABLE import_item_core_validations RENAME TO unavailable_preview_validations`)
	created, err := fixture.launcher.CreateReviewPreview(t.Context(), ReviewPreviewRequest{
		ImportItemID: fixture.itemID, ActorUserID: "reviewer", IdempotencyKey: "source-storage-fault",
	})
	var cause *sqlite.Error
	if !errors.As(err, &cause) || created.PreviewID != "" {
		t.Fatalf("source storage failure lost cause: created=%q error=%v", created.PreviewID, err)
	}
}

// This targets the old private restoration boundary, not the public initial replay query.
func TestPreviewRestoreQueryPreservesCancellation(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	preview := fixture.preview(t, "restore-source")
	if _, _, err := fixture.saver.CreateManual(t.Context(), preview.PreviewID, preview.Capability,
		"checkpoint", reviewCheckpointRequest(t, "point-B")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err := persistence.NewPreviewCreation(fixture.database).LoadPreviewRestore(ctx, preview.PreviewID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("private restore query cancellation lost: %v", err)
	}
}

func TestPreviewCreationAllowsUnchangedDraftRevision(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	clock := fixture.launcher.now
	changed := false
	fixture.launcher.now = func() time.Time {
		if !changed {
			changed = true
			mustRPGLaunchSQL(t, fixture.database, `UPDATE review_drafts SET version=version+1 WHERE import_item_id=?`, fixture.itemID)
		}
		return clock()
	}
	result, err := fixture.launcher.CreateReviewPreview(t.Context(), ReviewPreviewRequest{
		ImportItemID: fixture.itemID, ActorUserID: "reviewer", IdempotencyKey: "unchanged-draft-inputs",
	})
	if err != nil || !changed || result.PreviewID == "" {
		t.Fatalf("unchanged inputs rejected after draft revision: id=%q error=%v", result.PreviewID, err)
	}
}
