package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
)

func TestPegasusMigrationUpgradesVersion27AndPreservesImageAssets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	databasePath := filepath.Join(t.TempDir(), "retrom.db")
	legacy, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	if _, err := legacy.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 27)
	if _, err := legacy.ExecContext(ctx, `
PRAGMA defer_foreign_keys=ON;
BEGIN;
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES('01980000-0000-7000-8000-00000000a801',?,3,?,?,?,'image/png',1);
INSERT INTO game_metadata_revisions(id,game_id,title,description,developer,publisher,genre,players,
release_year,source_kind,source_ref_id,created_at_ms)
VALUES('01980000-0000-7000-8000-00000000a802','01980000-0000-7000-8000-00000000a804',
'Migration 028','','','','',NULL,NULL,'ADMIN_EDIT',NULL,1);
INSERT INTO game_content_revisions(id,game_id,content_kind,source_kind,source_ref_id,
source_manifest_json,source_manifest_digest,created_at_ms)
VALUES('01980000-0000-7000-8000-00000000a803','01980000-0000-7000-8000-00000000a804',
'SINGLE_FILE','ADMIN_REPLACE','migration-028','[]',?,1);
INSERT INTO games(id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,
search_text,version,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-00000000a804','01980000-0000-7000-8000-000000000001',
'PUBLISHED','01980000-0000-7000-8000-00000000a802','01980000-0000-7000-8000-00000000a803',
'migration 028',1,1,1);
INSERT INTO game_assets(id,game_id,metadata_revision_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms)
VALUES('01980000-0000-7000-8000-00000000a805','01980000-0000-7000-8000-00000000a804',
'01980000-0000-7000-8000-00000000a802','01980000-0000-7000-8000-00000000a801',
'COVER',0,320,480,'image/png',1);
COMMIT;
`, strings.Repeat("a", 64), strings.Repeat("b", 32), strings.Repeat("c", 40), strings.Repeat("d", 8),
		strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, databasePath, func() time.Time { return time.UnixMilli(3000) })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", upgraded.Close()) }()
	if err := upgraded.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
	var version, width, height int
	var kind, mediaType string
	if err := upgraded.SQL.QueryRow(`
SELECT (SELECT max(version) FROM schema_migrations),kind,width_px,height_px,media_type
FROM game_assets WHERE id='01980000-0000-7000-8000-00000000a805'
`).Scan(&version, &kind, &width, &height, &mediaType); err != nil || version != 36 || kind != "COVER" ||
		width != 320 || height != 480 || mediaType != "image/png" {
		t.Fatalf("upgrade = version:%d asset:%s/%d/%d/%s error:%v", version, kind, width, height, mediaType, err)
	}
	if _, err := upgraded.SQL.Exec(`
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES('01980000-0000-7000-8000-00000000a806',?,4,?,?,?,'video/mp4',2);
INSERT INTO game_assets(id,game_id,metadata_revision_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms)
VALUES('01980000-0000-7000-8000-00000000a807','01980000-0000-7000-8000-00000000a804',
'01980000-0000-7000-8000-00000000a802','01980000-0000-7000-8000-00000000a806',
'VIDEO',0,NULL,NULL,'video/mp4',2)
`, strings.Repeat("f", 64), strings.Repeat("1", 32), strings.Repeat("2", 40), strings.Repeat("3", 8)); err != nil {
		t.Fatalf("valid video asset: %v", err)
	}
	requireSQLFailure(t, upgraded.SQL, `
INSERT INTO game_assets(id,game_id,metadata_revision_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms)
VALUES('01980000-0000-7000-8000-00000000a808','01980000-0000-7000-8000-00000000a804',
'01980000-0000-7000-8000-00000000a802','01980000-0000-7000-8000-00000000a806',
'VIDEO',1,320,240,'video/mp4',2)`)
	requireSQLFailure(t, upgraded.SQL, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-00000000a809','SERVER_IMPORT','wrong','SERVER_PEGASUS_SCAN',?,1,
'{}',1,'QUEUED',0,4,1,1,1,1)`, strings.Repeat("4", 64))
}

func TestPegasusReviewHandoffMigrationPreservesVersion29Failures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	databasePath := filepath.Join(t.TempDir(), "retrom.db")
	legacy, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	if _, err := legacy.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 29)
	const userID = "01980000-0000-7000-8000-00000000b034"
	if _, err := legacy.ExecContext(ctx, `
INSERT INTO profiles(id,display_name,created_at_ms)
VALUES('01980000-0000-7000-8000-00000000b035','Migration 030',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'01980000-0000-7000-8000-00000000b035','migration030','Migration 030','ADMIN','ENABLED',1,1)
`, userID); err != nil {
		t.Fatal(err)
	}
	const importID = "01980000-0000-7000-8000-00000000b030"
	const scanJobID = "01980000-0000-7000-8000-00000000b031"
	const workJobID = "01980000-0000-7000-8000-00000000b032"
	const itemID = "01980000-0000-7000-8000-00000000b033"
	if _, err := legacy.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_SCAN',?,1,'{}',1,'SUCCEEDED',1,4,1,1,2,1,2),
      (?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_IMPORT',?,1,'{}',1,'SUCCEEDED',1,4,1,1,3,1,3)
`, scanJobID, importID, strings.Repeat("6", 64), workJobID, importID, strings.Repeat("7", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, `
INSERT INTO pegasus_imports(
  id,root_id,root_label_snapshot,source_relative_path,root_config_digest,state,phase,scan_job_id,
  import_job_id,game_count,processable_item_count,blocked_item_count,created_by_user_id,
  created_at_ms,updated_at_ms,completed_at_ms,expires_at_ms
) VALUES(?,'games','Games','FC',?,'PARTIAL_FAILURE',NULL,?,?,1,1,1,?,1,3,3,999999);
`, importID, strings.Repeat("8", 64), scanJobID, workJobID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, `
INSERT INTO pegasus_import_items(
  id,import_id,metadata_relative_path,game_ordinal,source_key,title,discovery_state,execution_state,
  metadata_json,source_manifest_json,source_manifest_digest,error_code,retryable,
  created_at_ms,updated_at_ms,completed_at_ms,error_details_json
) VALUES(?,?,'FC/metadata.pegasus.txt',0,?,'Legacy blocked game','BLOCKED_CONTENT','BLOCKED_CONTENT',
  '{"title":"Legacy blocked game"}','[]',?,'PEGASUS_CONTENT_FORMAT_UNSUPPORTED',0,1,3,3,
  '{"schemaVersion":1,"stage":"LIBRARY_IMPORT","technicalDetail":"preserve me"}')
`, itemID, importID, strings.Repeat("9", 64), strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, databasePath, func() time.Time { return time.UnixMilli(4000) })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", upgraded.Close()) }()
	if err := upgraded.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
	var version, reviewPending, reviewDiscarded int
	var title, executionState, details string
	if err := upgraded.SQL.QueryRow(`
SELECT (SELECT max(version) FROM schema_migrations),import.review_pending_item_count,
  import.review_discarded_item_count,item.title,item.execution_state,item.error_details_json
FROM pegasus_imports import
JOIN pegasus_import_items item ON item.import_id=import.id
WHERE import.id=? AND item.id=?
`, importID, itemID).Scan(
		&version, &reviewPending, &reviewDiscarded, &title, &executionState, &details,
	); err != nil {
		t.Fatal(err)
	}
	if version != 36 || reviewPending != 0 || reviewDiscarded != 0 ||
		title != "Legacy blocked game" || executionState != "BLOCKED_CONTENT" ||
		!strings.Contains(details, "preserve me") {
		t.Fatalf(
			"upgrade = version:%d reviews:%d/%d item:%q/%q details:%q",
			version, reviewPending, reviewDiscarded, title, executionState, details,
		)
	}
}

func TestPegasusReviewContentSnapshotMigrationUpgradesVersion34Trigger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	upgradedPath := filepath.Join(t.TempDir(), "upgraded.db")
	legacy, err := sql.Open("sqlite", upgradedPath)
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	if _, err := legacy.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 34)
	var oldTrigger string
	if err := legacy.QueryRow(`
SELECT sql FROM sqlite_master
WHERE type='trigger' AND name='game_content_revisions_pegasus_source_insert'
`).Scan(&oldTrigger); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(oldTrigger, "effective_source_snapshot_id") {
		t.Fatalf("version 34 trigger unexpectedly accepts review snapshots: %s", oldTrigger)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, upgradedPath, func() time.Time { return time.UnixMilli(5000) })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", upgraded.Close()) }()
	fresh, err := Open(ctx, filepath.Join(t.TempDir(), "fresh.db"), func() time.Time { return time.UnixMilli(5000) })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", fresh.Close()) }()
	for _, database := range []*DB{upgraded, fresh} {
		if err := database.IntegrityCheck(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var version int
	var upgradedTrigger, freshTrigger string
	if err := upgraded.SQL.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil || version != 36 {
		t.Fatalf("schema version = %d, error=%v", version, err)
	}
	for database, destination := range map[*DB]*string{
		upgraded: &upgradedTrigger,
		fresh:    &freshTrigger,
	} {
		if err := database.SQL.QueryRow(`
SELECT sql FROM sqlite_master
WHERE type='trigger' AND name='game_content_revisions_pegasus_source_insert'
`).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	if upgradedTrigger != freshTrigger ||
		!strings.Contains(upgradedTrigger, "effective_source_snapshot_id") ||
		!strings.Contains(upgradedTrigger, "item.library_import_item_id") {
		t.Fatalf("Pegasus content trigger drifted after upgrade:\n%s\n--- fresh ---\n%s", upgradedTrigger, freshTrigger)
	}
}
