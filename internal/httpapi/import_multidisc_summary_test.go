package httpapi

import (
	"database/sql"
	"testing"

	"retrom/internal/cleanup"
)

func TestMultiDiscSummaryUsesSelectedSourceNotNewestEvidence(t *testing.T) {
	t.Parallel()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close summary database", database.Close()) }()
	database.SetMaxOpenConns(1)
	_, err = database.ExecContext(t.Context(), `
CREATE TABLE import_items(id TEXT,import_job_id TEXT,state TEXT);
CREATE TABLE review_drafts(import_item_id TEXT,effective_source_snapshot_id TEXT);
CREATE TABLE import_item_source_snapshots(id TEXT,import_item_id TEXT,created_by TEXT,content_kind TEXT);
CREATE TABLE import_item_source_snapshot_files(source_snapshot_id TEXT,role TEXT,logical_name TEXT,upload_file_id TEXT);
CREATE TABLE upload_files(id TEXT,relative_path TEXT);
CREATE TABLE import_item_multidisc_entries(source_snapshot_id TEXT,ordinal INTEGER,state TEXT);
CREATE TABLE import_job_files(import_job_id TEXT,upload_file_id TEXT,disposition TEXT);
INSERT INTO import_items VALUES('item','job','REVIEW_PENDING');
INSERT INTO import_item_source_snapshots VALUES
('initial','item','IDENTIFICATION','MULTI_DISC'),
('selected','item','MULTIDISC_ATTACHMENT','MULTI_DISC'),
('unselected','item','MULTIDISC_ATTACHMENT','MULTI_DISC');
INSERT INTO upload_files VALUES('playlist','Game/list.m3u');
INSERT INTO import_item_source_snapshot_files VALUES
('initial','PLAYLIST_SOURCE','list.m3u','playlist'),
('selected','PLAYLIST_SOURCE','list.m3u','playlist'),
('unselected','PLAYLIST_SOURCE','list.m3u','playlist');
INSERT INTO import_item_multidisc_entries VALUES
('initial',0,'MISSING'),('selected',0,'PRESENT'),('selected',1,'PRESENT'),
('unselected',0,'MISSING');
`)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{database: database}
	initial, err := server.importMultiDiscItemSummaries(t.Context(), "job")
	if err != nil || len(initial) != 1 {
		t.Fatalf("initial summaries = %v, error = %v", initial, err)
	}
	if initial[0].MissingDiscCount != 1 || initial[0].PresentDiscCount != 0 {
		t.Fatalf("initial source not selected: %+v", initial[0])
	}
	if _, err := database.ExecContext(t.Context(), "INSERT INTO review_drafts VALUES('item','selected')"); err != nil {
		t.Fatal(err)
	}
	current, err := server.importMultiDiscItemSummaries(t.Context(), "job")
	if err != nil || len(current) != 1 {
		t.Fatalf("current summaries = %v, error = %v", current, err)
	}
	if current[0].DiscCount != 2 || current[0].PresentDiscCount != 2 || current[0].MissingDiscCount != 0 {
		t.Fatalf("summary followed unselected evidence: %+v", current[0])
	}
}
