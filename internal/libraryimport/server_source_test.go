package libraryimport

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"retrom/internal/testassert"

	_ "modernc.org/sqlite"
)

func TestServerImportResultKeepsLatestBlockedValidationWhenDraftHasNoSelection(t *testing.T) {
	t.Parallel()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "server-source.db"))
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.ExecContext(context.Background(), `
CREATE TABLE import_items(id TEXT PRIMARY KEY,import_job_id TEXT,state TEXT);
CREATE TABLE import_item_source_snapshots(id TEXT PRIMARY KEY,import_item_id TEXT,created_by TEXT,content_kind TEXT,source_manifest_json TEXT,source_manifest_digest TEXT);
CREATE TABLE review_drafts(import_item_id TEXT PRIMARY KEY,selected_validation_id TEXT,effective_source_snapshot_id TEXT,target_platform_instance_id TEXT);
CREATE TABLE import_item_core_validations(id TEXT PRIMARY KEY,import_item_id TEXT,target_platform_instance_id TEXT,source_snapshot_id TEXT,status TEXT,compatibility_code TEXT,core_id TEXT,dependency_snapshot_json TEXT,created_at_ms INTEGER);
CREATE TABLE cores(id TEXT PRIMARY KEY,name TEXT);
CREATE TABLE import_item_duplicate_matches(import_item_id TEXT,existing_game_id TEXT);
CREATE TABLE import_files(id TEXT PRIMARY KEY,relative_path TEXT);
CREATE TABLE import_item_source_files(import_item_id TEXT,upload_file_id TEXT,role TEXT);
CREATE TABLE import_job_files(import_job_id TEXT,disposition TEXT,reason_code TEXT);
INSERT INTO import_items VALUES('item','job','REVIEW_PENDING');
INSERT INTO import_item_source_snapshots VALUES('snapshot','item','IDENTIFICATION','SINGLE_FILE','{}','digest');
INSERT INTO review_drafts VALUES('item',NULL,'snapshot','platform');
INSERT INTO cores VALUES('fbneo','FinalBurn Neo');
INSERT INTO import_item_core_validations VALUES(
 'validation','item','platform','snapshot','BLOCKED','LAUNCH_PARENT_MISSING','fbneo',
 '{"schemaVersion":1,"kind":"ARCADE","machine":"1944j","missingEntries":["1944.zip"],"mismatchedEntries":[],"dependencies":[]}',2
);
INSERT INTO import_files VALUES('file','1944j.zip');
INSERT INTO import_item_source_files VALUES('item','file','CONTENT');
`); err != nil {
		t.Fatal(err)
	}
	result, err := (&Service{database: database}).serverImportResult(
		context.Background(), Created{ImportJobID: "job"},
	)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, len(result.Items) != 1, "items = %#v", result.Items)
	item := result.Items[0]
	testassert.Falsef(t, testassert.Any(func() bool { return item.ValidationStatus != "BLOCKED" }, func() bool { return item.CompatibilityCode != "LAUNCH_PARENT_MISSING" }, func() bool { return item.CoreID != "fbneo" }, func() bool { return item.CoreName != "FinalBurn Neo" }, func() bool { return item.DependencySnapshotJSON == "" }), "blocked item = %#v", item)
}
