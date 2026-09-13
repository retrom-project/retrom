package storageanalysis

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"retrom/internal/service/storageanalysis"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/store"
)

func TestAnalyzeClassifiesRegisteredCASAndReferenceViews(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if _, err := database.SQL.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	blobs := []testBlob{
		{"game", 100},
		{"bios", 200},
		{"save-state", 300},
		{"save-shot", 350},
		{"shared", 375},
		{"media", 400},
		{"workflow", 500},
		{"runtime", 600},
		{"game-archive", 700},
		{"game-member", 800},
		{"orphan-archive", 900},
		{"orphan-member", 900},
	}
	seedBlobs(t, database.SQL, blobs)
	seedReferences(t, database.SQL)
	fixed := time.UnixMilli(1_800_000_000_123)
	snapshot, err := storageanalysis.New(New(database.ReadOnly), func() time.Time { return fixed }).Analyze(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Scope != storageanalysis.Scope || snapshot.GeneratedAtMS != fixed.UnixMilli() {
		t.Fatalf("snapshot identity = %q/%d", snapshot.Scope, snapshot.GeneratedAtMS)
	}
	wantTotals := storageanalysis.Totals{RegisteredBytes: 6125, ProtectedBytes: 4325, UnreferencedBytes: 1800, BlobCount: 12}
	if snapshot.Totals != wantTotals {
		t.Fatalf("totals = %#v, want %#v", snapshot.Totals, wantTotals)
	}
	wantCategories := []storageanalysis.Category{
		{Code: storageanalysis.CategoryGameContent, Bytes: 1600, BlobCount: 3},
		{Code: storageanalysis.CategoryBIOS, Bytes: 200, BlobCount: 1},
		{Code: storageanalysis.CategorySaves, Bytes: 650, BlobCount: 2},
		{Code: storageanalysis.CategoryMedia, Bytes: 400, BlobCount: 1},
		{Code: storageanalysis.CategoryWorkflow, Bytes: 500, BlobCount: 1},
		{Code: storageanalysis.CategoryRuntimeSnapshot, Bytes: 600, BlobCount: 1},
		{Code: storageanalysis.CategorySharedDurable, Bytes: 375, BlobCount: 1},
		{Code: storageanalysis.CategoryOtherReferenced, Bytes: 0, BlobCount: 0},
		{Code: storageanalysis.CategoryUnreferenced, Bytes: 1800, BlobCount: 2},
	}
	if !reflect.DeepEqual(snapshot.Categories, wantCategories) {
		t.Fatalf("categories = %#v, want %#v", snapshot.Categories, wantCategories)
	}
	wantDetails := storageanalysis.Details{
		SaveStates: storageanalysis.SaveStateDetails{
			ActiveCount: 1, DeletedCount: 1, StateReferenceBytes: 675, ScreenshotReferenceBytes: 350,
		},
		CleanupCandidates: storageanalysis.CleanupCandidateDetails{BlobCount: 1, Bytes: 900},
	}
	if snapshot.Details != wantDetails {
		t.Fatalf("details = %#v, want %#v", snapshot.Details, wantDetails)
	}
	if !reflect.DeepEqual(snapshot.Excluded, storageanalysis.Excluded[:]) {
		t.Fatalf("excluded = %v", snapshot.Excluded)
	}
}

func TestAnalyzeSurfacesReadDatabaseFailure(t *testing.T) {
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReadOnly.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.SQL.Close()) })
	if _, err := storageanalysis.New(New(database.ReadOnly), time.Now).Analyze(context.Background()); err == nil {
		t.Fatal("Analyze succeeded with closed read-only database")
	}
}

type testBlob struct {
	id   string
	size int64
}

func seedBlobs(t *testing.T, database *sql.DB, blobs []testBlob) {
	t.Helper()
	for index, item := range blobs {
		value := index + 1
		if _, err := database.ExecContext(context.Background(), `
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES(?,?,?,?,?,?,?,0)`, item.id, fmt.Sprintf("%064x", value), item.size,
			fmt.Sprintf("%032x", value), fmt.Sprintf("%040x", value), fmt.Sprintf("%08x", value),
			"application/octet-stream"); err != nil {
			t.Fatalf("seed blob %s: %v", item.id, err)
		}
	}
}

func seedReferences(t *testing.T, database *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO game_files(game_id,role,logical_name,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order)
VALUES('content-rev','CONTENT','game.rom','game','game-archive',0,0),
('shared-rev','CONTENT','shared.rom','shared',NULL,NULL,0)`,
		`INSERT INTO variant_files(game_variant_id,role,logical_name,blob_id,sort_order)
VALUES('variant-rev','BIOS_BUNDLE','bios.zip','bios',0)`,
		`INSERT INTO game_assets(id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms)
VALUES('asset','game-id','media','COVER',0,1,1,'image/png',0)`,
		`INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,created_at_ms,updated_at_ms)
VALUES('upload-workflow','session','workflow.bin',500,500,'workflow','COMPLETE',0,0),
('upload-game','session','game.bin',100,100,'game','COMPLETE',0,0)`,
		`INSERT INTO launch_content_files(launch_session_id,logical_name,blob_id,format_version,created_at_ms)
VALUES('launch-runtime','runtime.rom','runtime','SOURCE_V1',0),
('launch-game','game.rom','game','SOURCE_V1',0)`,
		`INSERT INTO save_states(
id,profile_id,game_id,checkpoint_format,payload_blob_id,payload_sha256,payload_size_bytes,
screenshot_blob_id,name,active_duration_ms,dos_entry_path,version,created_at_ms,updated_at_ms,
deleted_at_ms,source_launch_session_id,disc_index)
VALUES('save-active','profile','game','checkpoint-v1','shared',printf('%064d',0),1,
'save-shot','Active',0,NULL,1,0,0,NULL,'launch-a',NULL),
('save-deleted','profile','game','checkpoint-v1','save-state',printf('%064d',0),1,
'save-shot','Deleted',0,NULL,1,0,0,10,'launch-b',NULL)`,
		`INSERT INTO archive_entries(archive_blob_id,ordinal,original_relative_path,normalized_path,ascii_casefold_path,archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,materialized_blob_id,created_at_ms)
VALUES('game-archive',0,'member.bin','member.bin','member.bin','ZIP','STORE',800,'00000001','00000000000000000000000000000001','0000000000000000000000000000000000000001','0000000000000000000000000000000000000000000000000000000000000001','game-member',0),
('orphan-archive',0,'orphan.bin','orphan.bin','orphan.bin','ZIP','STORE',900,'00000002','00000000000000000000000000000002','0000000000000000000000000000000000000002','0000000000000000000000000000000000000000000000000000000000000002','orphan-member',0)`,
		`INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES('orphan-gc','BLOB','orphan-archive','BLOB_GC','0000000000000000000000000000000000000000000000000000000000000001',1,'{}',0,'QUEUED',0,4,1,1,0,0)`,
		`INSERT INTO blob_gc_candidates(blob_id,gc_job_id,first_unreferenced_at_ms,scheduled_at_ms,attempt_count)
VALUES('orphan-archive','orphan-gc',0,1,0)`,
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("seed reference: %v\n%s", err, statement)
		}
	}
}
