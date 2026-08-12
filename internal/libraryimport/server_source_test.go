package libraryimport

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestServerImportResultKeepsLatestBlockedValidationWhenDraftHasNoSelection(t *testing.T) {
	t.Parallel()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "server-source.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`
CREATE TABLE import_items(id TEXT PRIMARY KEY,import_job_id TEXT,state TEXT);
CREATE TABLE import_item_source_snapshots(id TEXT PRIMARY KEY,import_item_id TEXT,revision_no INTEGER,content_kind TEXT,source_manifest_json TEXT,source_manifest_digest TEXT);
CREATE TABLE review_drafts(import_item_id TEXT PRIMARY KEY,selected_validation_id TEXT,effective_source_snapshot_id TEXT,target_platform_instance_id TEXT);
CREATE TABLE import_item_core_validations(id TEXT PRIMARY KEY,import_item_id TEXT,target_platform_instance_id TEXT,source_snapshot_id TEXT,status TEXT,compatibility_code TEXT,core_id TEXT,dependency_snapshot_json TEXT,created_at_ms INTEGER);
CREATE TABLE cores(id TEXT PRIMARY KEY,name TEXT);
CREATE TABLE import_item_duplicate_matches(import_item_id TEXT,existing_game_id TEXT,existing_game_content_revision_id TEXT);
CREATE TABLE upload_files(id TEXT PRIMARY KEY,relative_path TEXT);
CREATE TABLE import_item_source_files(import_item_id TEXT,upload_file_id TEXT,role TEXT);
CREATE TABLE import_job_files(import_job_id TEXT,disposition TEXT,reason_code TEXT);
INSERT INTO import_items VALUES('item','job','REVIEW_PENDING');
INSERT INTO import_item_source_snapshots VALUES('snapshot','item',1,'SINGLE_FILE','{}','digest');
INSERT INTO review_drafts VALUES('item',NULL,'snapshot','platform');
INSERT INTO cores VALUES('fbneo','FinalBurn Neo');
INSERT INTO import_item_core_validations VALUES(
 'validation','item','platform','snapshot','BLOCKED','LAUNCH_PARENT_MISSING','fbneo',
 '{"schemaVersion":2,"machine":"1944j","missingEntries":["1944.zip"],"mismatchedEntries":[],"dependencies":[]}',2
);
INSERT INTO upload_files VALUES('file','1944j.zip');
INSERT INTO import_item_source_files VALUES('item','file','CONTENT');
`); err != nil {
		t.Fatal(err)
	}
	result, err := (&Service{database: database}).serverImportResult(
		context.Background(), Created{ImportJobID: "job"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %#v", result.Items)
	}
	item := result.Items[0]
	if item.ValidationStatus != "BLOCKED" || item.CompatibilityCode != "LAUNCH_PARENT_MISSING" ||
		item.CoreID != "fbneo" || item.CoreName != "FinalBurn Neo" || item.DependencySnapshotJSON == "" {
		t.Fatalf("blocked item = %#v", item)
	}
}
