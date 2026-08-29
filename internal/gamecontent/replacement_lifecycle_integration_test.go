//go:build integration

package gamecontent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
)

func seedReplacementSave(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	blobs *blobstore.Store,
	gameID string,
) (string, string, []string) {
	t.Helper()
	var revisionID, contentID, artifactID, routeKey, dependencySnapshot, logicalName, contentBlobID string
	if err := database.QueryRowContext(ctx, `
SELECT variant.current_revision_id,revision.game_content_revision_id,revision.core_artifact_id,
       revision.route_key,revision.dependency_snapshot_json,file.logical_name,file.blob_id
FROM games game
JOIN game_variants variant ON variant.game_id=game.id
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
JOIN game_content_files file ON file.game_content_revision_id=revision.game_content_revision_id
WHERE game.id=? AND revision.game_content_revision_id=game.current_content_revision_id
ORDER BY file.sort_order,file.logical_name LIMIT 1
`, gameID).Scan(
		&revisionID, &contentID, &artifactID, &routeKey, &dependencySnapshot, &logicalName, &contentBlobID,
	); err != nil {
		t.Fatal(err)
	}
	profileID, launchID, saveID := mustReplacementID(t), mustReplacementID(t), mustReplacementID(t)
	now := time.Now().UnixMilli()
	if _, err := database.ExecContext(
		ctx, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,'Replacement player',?)`,
		profileID, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO launch_sessions(id,profile_id,purpose,game_id,game_content_revision_id,game_variant_revision_id,
core_artifact_id,route_key,return_to,credential_sha256,state,bootstrap_expires_at_ms,hard_expires_at_ms,
created_at_ms,updated_at_ms)
VALUES(?,?,'PRODUCT',?,?,?,?,?,'/',?,'CREATED',?,?,?,?)
`, launchID, profileID, gameID, contentID, revisionID, artifactID, routeKey, make([]byte, 32), now+60_000,
		now+120_000, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO launch_content_files(launch_session_id,logical_name,blob_id,format_version,created_at_ms)
VALUES(?,?,?,'SOURCE_V1',?)
`, launchID, logicalName, contentBlobID, now); err != nil {
		t.Fatal(err)
	}
	statePayload := []byte("state-" + saveID)
	stateBlobID := ensureReplacementBlob(t, ctx, database, blobs, statePayload)
	screenshotBlobID := ensureReplacementBlob(t, ctx, database, blobs, []byte("screenshot-"+saveID))
	stateDigest := sha256.Sum256(statePayload)
	dependencyDigest := sha256.Sum256([]byte(dependencySnapshot))
	if _, err := database.ExecContext(ctx, `
INSERT INTO save_states(id,profile_id,game_id,game_content_revision_id,game_variant_revision_id,
core_artifact_id,adapter_abi,save_abi,dependency_snapshot_sha256,payload_blob_id,payload_kind,payload_sha256,
payload_size_bytes,screenshot_blob_id,name,active_duration_ms,created_at_ms,updated_at_ms,source_launch_session_id)
VALUES(?,?,?,?,?,?,'emulatorjs-state-v1','emulatorjs-state-v1',?,?,'RUNTIME_STATE',?,?,?,'Before replacement',1000,?,?,?)
`, saveID, profileID, gameID, contentID, revisionID, artifactID,
		fmt.Sprintf("%x", dependencyDigest), stateBlobID, fmt.Sprintf("%x", stateDigest), len(statePayload),
		screenshotBlobID, now, now, launchID); err != nil {
		t.Fatal(err)
	}
	return saveID, launchID, []string{stateBlobID, screenshotBlobID}
}

func ensureReplacementBlob(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	blobs *blobstore.Store,
	contents []byte,
) string {
	t.Helper()
	metadata, err := blobs.Put(bytes.NewReader(contents))
	if err != nil {
		t.Fatal(err)
	}
	blobID, err := blobstore.EnsureRecord(
		ctx, database, metadata, "application/octet-stream", time.Now().UnixMilli(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return blobID
}

func assertReplacementFailure(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	jobID, wantedCode, gameID, wantedContentID, retainedSaveID string,
) {
	t.Helper()
	var code, contentID string
	var retryable bool
	if err := database.QueryRowContext(ctx, `SELECT error_code,error_retryable FROM jobs WHERE id=?`,
		jobID).Scan(&code, &retryable); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT current_content_revision_id FROM games WHERE id=?`,
		gameID).Scan(&contentID); err != nil {
		t.Fatal(err)
	}
	if code != wantedCode || retryable || contentID != wantedContentID {
		t.Fatalf("replacement failure = %s/%t/%s, want %s/false/%s",
			code, retryable, contentID, wantedCode, wantedContentID)
	}
	if retainedSaveID == "" {
		return
	}
	var count int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM save_states WHERE id=?`,
		retainedSaveID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unchanged replacement save count = %d, error=%v", count, err)
	}
}

func assertSupersededContentReleased(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	gameID, oldContentID, saveID, launchID string,
	savePayloads []string,
) {
	t.Helper()
	assertContentPayloadCount(t, ctx, database, oldContentID, 0)
	var saves, launchFiles int
	var launchState string
	if err := database.QueryRowContext(ctx, `
SELECT
 (SELECT count(*) FROM save_states WHERE id=?),
 (SELECT state FROM launch_sessions WHERE id=?),
 (SELECT count(*) FROM launch_content_files WHERE launch_session_id=?)
`, saveID, launchID, launchID).Scan(&saves, &launchState, &launchFiles); err != nil {
		t.Fatal(err)
	}
	if saves != 0 || launchState != "REVOKED" || launchFiles != 0 {
		t.Fatalf("retired lifecycle = saves %d, launch %s, launch files %d", saves, launchState, launchFiles)
	}
	for _, blobID := range savePayloads {
		var candidates int
		if err := database.QueryRowContext(
			ctx, `SELECT count(*) FROM blob_gc_candidates WHERE blob_id=?`, blobID,
		).Scan(&candidates); err != nil || candidates != 1 {
			t.Fatalf("save payload %s GC candidates = %d, error=%v", blobID, candidates, err)
		}
	}
	var auditRows int
	if err := database.QueryRowContext(
		ctx, `SELECT count(*) FROM game_content_revisions WHERE id=? AND game_id=?`, oldContentID, gameID,
	).Scan(&auditRows); err != nil || auditRows != 1 {
		t.Fatalf("old content audit rows = %d, error=%v", auditRows, err)
	}
}

func assertContentPayloadCount(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	contentID string,
	wanted int,
) {
	t.Helper()
	var count int
	if err := database.QueryRowContext(
		ctx, `SELECT count(*) FROM game_content_files WHERE game_content_revision_id=?`, contentID,
	).Scan(&count); err != nil || count != wanted {
		t.Fatalf("content %s payload count = %d, want %d, error=%v", contentID, count, wanted, err)
	}
}

func assertBlobReferenceState(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	blobID string,
	wantedCurrent bool,
) {
	t.Helper()
	var currentReferences int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM game_content_files file
JOIN game_content_revisions revision ON revision.id=file.game_content_revision_id
JOIN games game ON game.current_content_revision_id=revision.id
WHERE file.blob_id=?
`, blobID).Scan(&currentReferences); err != nil {
		t.Fatal(err)
	}
	if (currentReferences > 0) != wantedCurrent {
		t.Fatalf("blob %s current reference count = %d, wanted current=%t",
			blobID, currentReferences, wantedCurrent)
	}
}

func mustReplacementID(t *testing.T) string {
	t.Helper()
	value, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return value.String()
}
