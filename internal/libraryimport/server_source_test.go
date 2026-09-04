package libraryimport

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/testassert"

	_ "modernc.org/sqlite"
)

func TestNormalizeServerReviewMetadataUsesOrdinaryDraftLimits(t *testing.T) {
	t.Parallel()
	releaseYear := 1949
	metadata, warnings, err := normalizeServerReviewMetadata(ServerMetadata{
		Title:       "Fixture",
		Description: strings.Repeat("界", reviewDescriptionMaximumRunes+1),
		Developer:   strings.Repeat("开", reviewShortFieldMaximumRunes+1),
		Publisher:   strings.Repeat("发", reviewShortFieldMaximumRunes+1),
		Genre:       strings.Repeat("类", reviewShortFieldMaximumRunes+1),
		ReleaseYear: &releaseYear,
	}, 2027)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return len([]rune(metadata.Description)) != reviewDescriptionMaximumRunes }, func() bool { return len([]rune(metadata.Developer)) != reviewShortFieldMaximumRunes }, func() bool { return len([]rune(metadata.Publisher)) != reviewShortFieldMaximumRunes }, func() bool { return len([]rune(metadata.Genre)) != reviewShortFieldMaximumRunes }, func() bool { return metadata.ReleaseYear != nil }), "normalized metadata = %#v", metadata)
	expected := []ServerMetadataWarning{
		{Code: "FIELD_TRUNCATED", Field: "description"},
		{Code: "FIELD_TRUNCATED", Field: "developer"},
		{Code: "FIELD_TRUNCATED", Field: "publisher"},
		{Code: "FIELD_TRUNCATED", Field: "genre"},
		{Code: "FIELD_VALUE_INVALID", Field: "releaseYear"},
	}
	testassert.Falsef(t, len(warnings) != len(expected), "warnings = %#v", warnings)
	for index := range expected {
		testassert.Falsef(t, warnings[index] != expected[index], "warnings[%d] = %#v, want %#v", index, warnings[index], expected[index])
	}
}

func TestServerImportResultKeepsLatestBlockedValidationWhenDraftHasNoSelection(t *testing.T) {
	t.Parallel()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "server-source.db"))
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.ExecContext(context.Background(), `
CREATE TABLE import_items(id TEXT PRIMARY KEY,import_job_id TEXT,state TEXT);
CREATE TABLE import_item_source_snapshots(id TEXT PRIMARY KEY,import_item_id TEXT,revision_no INTEGER,content_kind TEXT,source_manifest_json TEXT,source_manifest_digest TEXT);
CREATE TABLE review_drafts(import_item_id TEXT PRIMARY KEY,selected_validation_id TEXT,effective_source_snapshot_id TEXT,target_platform_instance_id TEXT);
CREATE TABLE import_item_core_validations(id TEXT PRIMARY KEY,import_item_id TEXT,target_platform_instance_id TEXT,source_snapshot_id TEXT,status TEXT,compatibility_code TEXT,core_id TEXT,dependency_snapshot_json TEXT,created_at_ms INTEGER);
CREATE TABLE cores(id TEXT PRIMARY KEY,name TEXT);
CREATE TABLE import_item_duplicate_matches(import_item_id TEXT,existing_game_id TEXT);
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
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, len(result.Items) != 1, "items = %#v", result.Items)
	item := result.Items[0]
	testassert.Falsef(t, testassert.Any(func() bool { return item.ValidationStatus != "BLOCKED" }, func() bool { return item.CompatibilityCode != "LAUNCH_PARENT_MISSING" }, func() bool { return item.CoreID != "fbneo" }, func() bool { return item.CoreName != "FinalBurn Neo" }, func() bool { return item.DependencySnapshotJSON == "" }), "blocked item = %#v", item)
}
