//go:build integration

package gamecontent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"retrom/internal/persistence/recordstore"

	dbapi "retrom/internal/database"

	"github.com/google/uuid"

	"retrom/internal/filestore"
)

func seedReplacementSave(
	t *testing.T,
	ctx context.Context,
	database dbapi.DB,
	blobs *filestore.Store,
	gameID string,
) (string, string, []string) {
	t.Helper()
	var variantID, providerID, targetID, coreID, compatibilityCode, contentKind string
	var bundleSHA256, checkpointFormat, dependencySnapshot, logicalName, contentFileRecord string
	if err := dbapi.QueryRowContext(ctx, database, `
SELECT variant.id,variant.provider_id,variant.target_id,variant.core_id,
       variant.compatibility_code,game.content_kind,
       provider.bundle_sha256,((target.checkpoint_json)::jsonb #>> '{writeFormat}'),
       variant.dependency_snapshot_json,file.logical_name,file.file_record
FROM games game
JOIN game_variants variant ON variant.game_id=game.id
JOIN runtime_providers provider ON provider.provider_id=variant.provider_id
JOIN runtime_targets target ON target.provider_id=variant.provider_id AND target.target_id=variant.target_id
JOIN game_files file ON file.game_id=game.id
WHERE game.id=?
ORDER BY file.sort_order,file.logical_name LIMIT 1
`, gameID).Scan(
		&variantID, &providerID, &targetID, &coreID, &compatibilityCode, &contentKind,
		&bundleSHA256, &checkpointFormat, &dependencySnapshot, &logicalName, &contentFileRecord,
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
INSERT INTO launch_sessions(id,profile_id,game_id,core_id,provider_id,target_id,
bundle_sha256,content_kind,dependency_snapshot_json,compatibility_code,
return_to,credential_sha256,state,bootstrap_expires_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,'/',?,'CREATED',?,?,?,?)
`, launchID, profileID, gameID, coreID, providerID, targetID, bundleSHA256, contentKind,
		dependencySnapshot, compatibilityCode, make([]byte, 32), now+60_000,
		now+120_000, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO launch_content_files(launch_session_id,logical_name,file_record,format_version,created_at_ms)
VALUES(?,?,?,'SOURCE_V1',?)
`, launchID, logicalName, contentFileRecord, now); err != nil {
		t.Fatal(err)
	}
	statePayload := []byte("state-" + saveID)
	stateFileRecord := ensureReplacementBlob(t, ctx, database, blobs, statePayload)
	screenshotFileRecord := ensureReplacementBlob(t, ctx, database, blobs, []byte("screenshot-"+saveID))
	stateCopy, err := blobs.CopyTo(ctx, stateFileRecord, "saves/"+saveID+"/"+saveID, "payload")
	if err != nil {
		t.Fatal(err)
	}
	imageCopy, err := blobs.CopyTo(ctx, screenshotFileRecord, "saves/"+saveID+"/"+saveID, "screenshot")
	if err != nil {
		t.Fatal(err)
	}
	stateFileRecord, screenshotFileRecord = stateCopy.Record, imageCopy.Record
	stateDigest := sha256.Sum256(statePayload)
	if _, err := recordstore.InsertRows(ctx, database, "save_states", `
INSERT INTO save_states(id,profile_id,game_id,checkpoint_format,payload_file_record,payload_sha256,
payload_size_bytes,screenshot_file_record,name,active_duration_ms,created_at_ms,updated_at_ms,
source_launch_session_id)
VALUES(?,?,?,?,?,?,?,?,'Before replacement',1000,?,?,?)
`, saveID, profileID, gameID, checkpointFormat, stateFileRecord, fmt.Sprintf("%x", stateDigest), len(statePayload), screenshotFileRecord, now, now, launchID); err != nil {
		t.Fatal(err)
	}

	return saveID, launchID, []string{stateFileRecord, screenshotFileRecord}
}

func ensureReplacementBlob(
	t *testing.T,
	ctx context.Context,
	database dbapi.DB,
	blobs *filestore.Store,
	contents []byte,
) string {
	t.Helper()
	metadata, err := blobs.Put(bytes.NewReader(contents))
	if err != nil {
		t.Fatal(err)
	}
	fileRecord, err := filestore.FileRecord(metadata, "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	return fileRecord
}

func assertReplacementFailure(
	t *testing.T,
	ctx context.Context,
	database dbapi.DB,
	jobID, wantedCode, gameID, wantedContentID, retainedSaveID string,
) {
	t.Helper()
	var code, contentID string
	var retryable bool
	if err := dbapi.QueryRowContext(ctx, database, `SELECT error_code,error_retryable FROM jobs WHERE id=?`,
		jobID).Scan(&code, &retryable); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.QueryRowContext(ctx, database, `SELECT id FROM games WHERE id=?`,
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
	if err := dbapi.QueryRowContext(ctx, database, `SELECT count(*) FROM save_states WHERE id=?`,
		retainedSaveID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unchanged replacement save count = %d, error=%v", count, err)
	}
}

func assertSupersededContentReleased(
	t *testing.T,
	ctx context.Context,
	database dbapi.DB,
	gameID, oldContentID, saveID, launchID string,
	savePayloads []string,
) {
	t.Helper()
	if oldContentID != gameID {
		t.Fatalf("current-state replacement changed game identity: %s != %s", oldContentID, gameID)
	}
	var saves, launchFiles int
	var launchState string
	if err := dbapi.QueryRowContext(ctx, database, `
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
	for _, fileRecord := range savePayloads {
		var candidates int
		if err := dbapi.QueryRowContext(
			ctx, database,
			`SELECT count(*) FROM job_input_snapshots WHERE ((?)::jsonb #>> '{path}') LIKE ((input_json)::jsonb #>> '{inputs,relativePath}') || '/%'`, fileRecord,
		).Scan(&candidates); err != nil || candidates != 1 {
			t.Fatalf("save payload %s DeletionQueue candidates = %d, error=%v", fileRecord, candidates, err)
		}
	}
}

func assertContentPayloadCount(
	t *testing.T,
	ctx context.Context,
	database dbapi.DB,
	contentID string,
	wanted int,
) {
	t.Helper()
	var count int
	if err := dbapi.QueryRowContext(
		ctx, database,
		`SELECT count(*) FROM game_files WHERE game_id=?`, contentID,
	).Scan(&count); err != nil || count != wanted {
		t.Fatalf("content %s payload count = %d, want %d, error=%v", contentID, count, wanted, err)
	}
}

func assertBlobReferenceState(
	t *testing.T,
	ctx context.Context,
	database dbapi.DB,
	fileRecord string,
	wantedCurrent bool,
) {
	t.Helper()
	var currentReferences int
	if err := dbapi.QueryRowContext(ctx, database, `
SELECT count(*) FROM game_files WHERE file_record=?
`, fileRecord).Scan(&currentReferences); err != nil {
		t.Fatal(err)
	}
	if (currentReferences > 0) != wantedCurrent {
		t.Fatalf("blob %s current reference count = %d, wanted current=%t",
			fileRecord, currentReferences, wantedCurrent)
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
