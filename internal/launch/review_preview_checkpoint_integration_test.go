//go:build integration

package launch

import (
	"bytes"
	"database/sql"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/libraryimport"
	"retrom/internal/payloadrelease"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/saves"
	"retrom/internal/testsupport"
)

type reviewCheckpointFixture struct {
	database *sql.DB
	launcher *Service
	saver    *saves.Service
	releaser *payloadrelease.Service
	now      *time.Time
	itemID   string
}

func newReviewCheckpointFixture(t *testing.T) reviewCheckpointFixture {
	t.Helper()
	now := time.UnixMilli(1_786_000_000_000)
	clock := func() time.Time { return now }
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(dataDir, "review.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	seedLocalProfile(t, database.SQL)
	source := seedRPGReviewFixture(t, database.SQL, now.UnixMilli())
	mustRPGLaunchSQL(t, database.SQL, `
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('reviewer','local','reviewer','Reviewer','ADMIN','ENABLED',0,0)`)
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	releaser, err := payloadrelease.New(database.SQL, blobs, clock, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return reviewCheckpointFixture{
		database: database.SQL, now: &now, itemID: source.itemID,
		launcher: newRPGReviewLaunchService(t, t.Context(), database.SQL, credentials, clock),
		saver:    saves.New(database.SQL, blobs, credentials, clock), releaser: releaser,
	}
}

func (fixture reviewCheckpointFixture) preview(t *testing.T, key string) ReviewPreviewCreated {
	t.Helper()
	preview, err := fixture.launcher.CreateReviewPreview(t.Context(), ReviewPreviewRequest{
		ImportItemID: fixture.itemID, ActorUserID: "reviewer", IdempotencyKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.launcher.ReviewPreviewConfig(t.Context(), preview.PreviewID, preview.Capability); err != nil {
		t.Fatal(err)
	}
	return preview
}

func reviewCheckpointRequest(t *testing.T, contents string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("metadata", `{"checkpointFormat":"test-checkpoint-v1","name":"Review checkpoint"}`); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("payload", "checkpoint.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(contents)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestOrdinaryReviewPlayerCanReplaceItsTemporaryCheckpoint(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	preview := fixture.preview(t, "trial")
	for _, state := range []string{"point-B", "point-C"} {
		result, replayed, err := fixture.saver.CreateManual(t.Context(), preview.PreviewID, preview.Capability,
			state, reviewCheckpointRequest(t, state))
		if err != nil || replayed || result.CheckpointFormat != "test-checkpoint-v1" {
			t.Fatalf("ordinary trial checkpoint failed: %+v replay=%v error=%v", result, replayed, err)
		}
	}
	var products int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM games)+(SELECT count(*) FROM save_states)`).Scan(&products); err != nil {
		t.Fatal(err)
	}
	if products != 0 {
		t.Fatal("temporary trial checkpoint leaked into product games/saves")
	}
}

func TestReviewRestoreFreezesTheCheckpointWithoutAnOriginalCloseGate(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	original := fixture.preview(t, "original")
	if _, _, err := fixture.saver.CreateManual(t.Context(), original.PreviewID, original.Capability,
		"point-B", reviewCheckpointRequest(t, "point-B")); err != nil {
		t.Fatal(err)
	}
	restored, err := fixture.launcher.CreateReviewPreview(t.Context(), ReviewPreviewRequest{
		ImportItemID: fixture.itemID, ActorUserID: "reviewer", IdempotencyKey: "restore-B",
		RestoreFromPreviewID: &original.PreviewID,
	})
	if err != nil || restored.PreviewID == original.PreviewID {
		t.Fatalf("ordinary restore preview not created: %+v %v", restored, err)
	}
	configuration, err := fixture.launcher.ReviewPreviewConfig(t.Context(), restored.PreviewID, restored.Capability)
	if err != nil {
		t.Fatal(err)
	}
	restore := testsupport.RuntimeEnvelopeObject(t, testsupport.RuntimeEnvelope(t, configuration), "restore")
	if restore["format"] != "test-checkpoint-v1" || restore["url"] != "/runtime/launches/"+restored.PreviewID+"/state" {
		t.Fatalf("ordinary review restore missing: %+v", restore)
	}
	if _, _, err := fixture.saver.CreateManual(t.Context(), original.PreviewID, original.Capability,
		"point-C", reviewCheckpointRequest(t, "point-C")); err != nil {
		t.Fatal(err)
	}
	digest, err := fixture.saver.StateDigest(t.Context(), restored.PreviewID, restored.Capability)
	if err != nil || digest != restore["sha256"] {
		t.Fatalf("new preview followed mutable original checkpoint instead of frozen B: %s %v", digest, err)
	}
}

func TestReviewCheckpointRejectsCrossSessionIdempotencyReplay(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	original := fixture.preview(t, "original")
	another := fixture.preview(t, "another")
	result, replayed, err := fixture.saver.CreateManual(t.Context(), original.PreviewID, original.Capability,
		"save-once", reviewCheckpointRequest(t, "point-B"))
	if err != nil || replayed || result.PreviewID != original.PreviewID {
		t.Fatalf("create trial checkpoint: %+v %v", result, err)
	}
	repeated, replayed, err := fixture.saver.CreateManual(t.Context(), original.PreviewID, original.Capability,
		"save-once", reviewCheckpointRequest(t, "point-B"))
	if err != nil || !replayed || repeated.PreviewID != original.PreviewID {
		t.Fatalf("idempotent trial checkpoint: %+v %v", repeated, err)
	}
	if _, _, err := fixture.saver.CreateManual(t.Context(), another.PreviewID, another.Capability,
		"save-once", reviewCheckpointRequest(t, "point-B")); !errors.Is(err, saves.ErrSequenceReused) {
		t.Fatalf("save key replayed across preview sessions: %v", err)
	}
}

func TestReviewCheckpointIsScopedExpiringAndReleasedByOrdinaryGC(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	original := fixture.preview(t, "original")
	if _, _, err := fixture.saver.CreateManual(t.Context(), original.PreviewID, original.Capability,
		"point-B", reviewCheckpointRequest(t, "point-B")); err != nil {
		t.Fatal(err)
	}
	request := ReviewPreviewRequest{
		ImportItemID: fixture.itemID, ActorUserID: "someone-else", IdempotencyKey: "restore",
		RestoreFromPreviewID: &original.PreviewID,
	}
	if _, err := fixture.launcher.CreateReviewPreview(t.Context(), request); !errors.Is(err, ErrSaveIncompatible) {
		t.Fatalf("another reviewer can read a trial checkpoint: %v", err)
	}
	request.ActorUserID = "reviewer"
	restored, err := fixture.launcher.CreateReviewPreview(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.RestoreFromPreviewID = nil
	if _, err := fixture.launcher.CreateReviewPreview(t.Context(), request); !errors.Is(err, ErrReviewPreviewUnavailable) {
		t.Fatalf("preview replay changed restore selection: %v", err)
	}
	*fixture.now = fixture.now.Add(3 * time.Hour)
	request.IdempotencyKey, request.RestoreFromPreviewID = "expired", &original.PreviewID
	if _, err := fixture.launcher.CreateReviewPreview(t.Context(), request); !errors.Is(err, ErrSaveIncompatible) {
		t.Fatalf("expired checkpoint can start another restore: %v", err)
	}
	if _, _, err := fixture.saver.CreateManual(t.Context(), original.PreviewID, original.Capability,
		"expired", reviewCheckpointRequest(t, "point-C")); !errors.Is(err, saves.ErrCredential) {
		t.Fatalf("expired trial can write checkpoint: %v", err)
	}
	if err := fixture.releaser.ReconcileGC(t.Context()); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := fixture.database.QueryRowContext(t.Context(), `
SELECT count(*) FROM review_preview_sessions WHERE id IN (?,?)
 AND (state<>'EXPIRED' OR checkpoint_payload_blob_id IS NOT NULL OR restore_payload_blob_id IS NOT NULL)
`, original.PreviewID, restored.PreviewID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("expired trial retained checkpoint references: %d %v", remaining, err)
	}
}

func TestPublishingReviewReleasesAllTemporaryPreviewOwners(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	preview := fixture.preview(t, "publication")
	if _, _, err := fixture.saver.CreateManual(t.Context(), preview.PreviewID, preview.Capability,
		"checkpoint", reviewCheckpointRequest(t, "temporary")); err != nil {
		t.Fatal(err)
	}
	mustRPGLaunchSQL(t, fixture.database,
		`UPDATE review_drafts SET metadata_json='{"title":"Published trial"}' WHERE import_item_id=?`, fixture.itemID)
	approved, err := libraryimport.New(fixture.database, func() time.Time { return *fixture.now }).
		Approve(t.Context(), fixture.itemID, 2)
	if err != nil || approved.GameID == "" {
		t.Fatalf("publish ordinary review: %+v %v", approved, err)
	}
	for range 10 {
		found, err := fixture.releaser.RunOnce(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			break
		}
	}
	var previews, productSaves int
	var payloadState string
	if err := fixture.database.QueryRowContext(t.Context(), `
SELECT (SELECT count(*) FROM review_preview_sessions WHERE import_item_id=?),
 (SELECT count(*) FROM save_states),payload_state FROM import_items WHERE id=?`,
		fixture.itemID, fixture.itemID).Scan(&previews, &productSaves, &payloadState); err != nil {
		t.Fatal(err)
	}
	if previews != 0 || productSaves != 0 || payloadState != "RELEASED" {
		t.Fatalf("publication retained temporary owners: previews=%d saves=%d payload=%s", previews, productSaves, payloadState)
	}
}
