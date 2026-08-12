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
`).Scan(&version, &kind, &width, &height, &mediaType); err != nil || version != 28 || kind != "COVER" ||
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
