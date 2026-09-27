package metadatascrape

import (
	"errors"
	"strings"
	"testing"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/metadatascrape"
)

func TestReplacingScrapeRemovesCandidatesAndFencesOldMedia(t *testing.T) {
	t.Parallel()
	fixture := newMediaFixture(t)
	oldJob := fixture.jobID
	seedScrapeOwnedFiles(t, fixture.database)
	now := fixture.now.UnixMilli()
	plan := application.SchedulePlan{Subject: application.Subject{Kind: "GAME", ID: "game"}, RunID: "replacement", JobID: "replacement-job", Provider: "NONE", Dedupe: strings.Repeat("e", 64), PayloadJSON: "{}", JobState: "SUCCEEDED", RunState: "COMPLETED", EventJSON: "{}", Now: now, FinishedAt: &now}
	if err := NewScheduler(fixture.database).WithWrite(t.Context(), func(scope application.ScheduleScope) error { return scope.Writes.Create(t.Context(), plan) }); err != nil {
		t.Fatal(err)
	}
	assertScrapeFileRetirement(t, fixture.database, true)
	var runs, candidates, assets, evidence, attempts, media int
	err := dbapi.QueryRowContext(t.Context(), fixture.database, `SELECT
 (SELECT count(*) FROM metadata_scrape_runs WHERE game_id='game'),
 (SELECT count(*) FROM scrape_candidates),(SELECT count(*) FROM scrape_candidate_assets),
 (SELECT count(*) FROM content_hash_evidence),(SELECT count(*) FROM metadata_scrape_query_attempts),
 (SELECT count(*) FROM metadata_media_runs)`).Scan(&runs, &candidates, &assets, &evidence, &attempts, &media)
	if err != nil || runs != 1 || candidates+assets+evidence+attempts+media != 0 {
		t.Fatalf("replacement: runs=%d candidates=%d assets=%d evidence=%d attempts=%d media=%d err=%v", runs, candidates, assets, evidence, attempts, media, err)
	}
	var state, reason string
	if err := dbapi.QueryRowContext(t.Context(), fixture.database, "SELECT state,cancel_reason FROM jobs WHERE id=?", oldJob).Scan(&state, &reason); err != nil || state != "CANCELLED" || reason != "SCRAPE_REPLACED" {
		t.Fatalf("old media: %s %s %v", state, reason, err)
	}
	// The database enforces one current result even for callers bypassing scheduling.
	_, err = fixture.database.ExecContext(t.Context(), `INSERT INTO metadata_scrape_runs
 (id,game_id,job_id,provider,provider_config_version,state,created_at_ms,updated_at_ms,completed_at_ms)
 VALUES('illegal-history','game','job','NONE',1,'COMPLETED',?,?,?)`, now, now, now)
	if err == nil {
		t.Fatal("accepted a second scrape version")
	}
}

func TestReplacingScrapeRollsBackCandidatesAndJobsOnFailure(t *testing.T) {
	t.Parallel()
	fixture := newMediaFixture(t)
	seedScrapeOwnedFiles(t, fixture.database)
	cause := errors.New("failed after replacement")
	err := NewScheduler(fixture.database).WithWrite(t.Context(), func(scope application.ScheduleScope) error {
		plan := application.SchedulePlan{Subject: application.Subject{Kind: "GAME", ID: "game"}, RunID: "replacement", JobID: "replacement-job", Provider: "HASHEOUS", Dedupe: strings.Repeat("e", 64), PayloadJSON: "{}", JobState: "QUEUED", RunState: "RUNNING", EventJSON: "{}", Now: fixture.now.UnixMilli()}
		if err := scope.Writes.Create(t.Context(), plan); err != nil {
			return err
		}
		return cause
	})
	if !errors.Is(err, cause) {
		t.Fatal(err)
	}
	assertScrapeFileRetirement(t, fixture.database, false)
	var runs, assets int
	var state string
	err = dbapi.QueryRowContext(t.Context(), fixture.database, `SELECT
 (SELECT count(*) FROM metadata_scrape_runs WHERE id='run'),
 (SELECT count(*) FROM scrape_candidate_assets),state FROM jobs WHERE id=?`, fixture.jobID).Scan(&runs, &assets, &state)
	if err != nil || runs != 1 || assets != 1 || state != "QUEUED" {
		t.Fatalf("partial replacement: %d %d %s %v", runs, assets, state, err)
	}
}

func seedScrapeOwnedFiles(t *testing.T, db dbapi.DB) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `INSERT INTO stored_files(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms,owner_kind,owner_id)
VALUES('unused-media',?,1,?,?,?,'image/png',0,'SCRAPE_RUN','run'),('selected-media',?,1,?,?,?,'image/png',0,'GAME','game')`, strings.Repeat("a", 64), strings.Repeat("a", 32), strings.Repeat("a", 40), strings.Repeat("a", 8), strings.Repeat("a", 64), strings.Repeat("a", 32), strings.Repeat("a", 40), strings.Repeat("a", 8))
	if err != nil {
		t.Fatal(err)
	}
}

func assertScrapeFileRetirement(t *testing.T, db dbapi.DB, expected bool) {
	t.Helper()
	var retired, selected bool
	err := dbapi.QueryRowContext(t.Context(), db, `SELECT (SELECT retired_at_ms IS NOT NULL FROM stored_files WHERE id='unused-media'),(SELECT retired_at_ms IS NOT NULL FROM stored_files WHERE id='selected-media')`).Scan(&retired, &selected)
	if err != nil || retired != expected || selected {
		t.Fatalf("scrape retirement=%v selected=%v err=%v", retired, selected, err)
	}
}
