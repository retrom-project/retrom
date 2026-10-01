//go:build integration

package libraryimport

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"retrom/internal/hasheous"
	"retrom/internal/legacychecksum"

	dbapi "retrom/internal/database"
	scrapepersistence "retrom/internal/persistence/metadatascrape"
	scrapeservice "retrom/internal/service/metadatascrape"
)

func assertParentArchiveIndexes(t *testing.T, database dbapi.DB, kind, id, want string) {
	t.Helper()
	table, column := "import_item_source_snapshot_files", "source_snapshot_id"
	if kind == "game" {
		table, column = "game_files", "game_id"
	}
	values := queryAttachmentStrings(t, database, `SELECT f.role||':'||f.logical_name||':'||
 (SELECT count(*) FROM archive_entries e WHERE e.archive_file_record=f.file_record)
 FROM `+table+` f WHERE f.`+column+`=? ORDER BY f.role,f.logical_name`, id)
	if fmt.Sprint(values) != want {
		t.Fatalf("%s archive indexes = %v, want %s", kind, values, want)
	}
}

func assertPublishedParentScrapeEvidence(t *testing.T, database dbapi.DB, gameID string) {
	t.Helper()
	var version int64
	if err := dbapi.QueryRowContext(t.Context(), database, "SELECT version FROM games WHERE id=?", gameID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	scheduled, _, err := scrapeservice.NewScheduler(scrapepersistence.NewScheduler(database), nil, time.Now).
		ScheduleGame(t.Context(), gameID, version)
	if err != nil {
		t.Fatal(err)
	}
	values := queryAttachmentStrings(t, database, `SELECT f.role||':'||f.logical_name||':'||e.normalized_path
 FROM content_hash_evidence h JOIN archive_entries e ON e.archive_file_record=h.archive_file_record
 AND e.ordinal=h.archive_entry_ordinal JOIN game_files f ON f.file_record=e.archive_file_record
 WHERE h.scrape_run_id=? AND f.game_id=? ORDER BY h.query_order`, scheduled.RunID, gameID)
	if fmt.Sprint(values) != "[CONTENT:a.zip:a.bin]" {
		t.Fatalf("published rescrape evidence = %v", values)
	}
	runParentScrape(t, database, scheduled.RunID, 1)
	assertEmptyAndMissingParentScrape(t, database, gameID, version+1)
}

type parentScrapeLookup struct {
	t     *testing.T
	calls int
}

func (lookup *parentScrapeLookup) Lookup(_ context.Context, hashes hasheous.ContentHashes, bypass bool) (scrapeservice.ResolvedLookup, error) {
	lookup.t.Helper()
	_, expectedSHA1 := legacychecksum.Sum([]byte("child"))
	if !bypass || hashes.SHA1 != expectedSHA1 {
		lookup.t.Fatalf("lookup must query child member with cache bypass: %+v bypass=%v", hashes, bypass)
	}
	lookup.calls++
	digest, err := hasheous.RequestDigest(hashes)
	return scrapeservice.ResolvedLookup{Result: hasheous.LookupResult{
		RequestDigest: digest, Outcome: hasheous.OutcomeMiss, HTTPStatus: 404,
	}}, err
}

func runParentScrape(t *testing.T, database dbapi.DB, runID string, want int) {
	t.Helper()
	lookup := &parentScrapeLookup{t: t}
	repository := scrapepersistence.NewWorker(database)
	recorder := scrapeservice.NewRecorder(scrapepersistence.NewRecorder(database), nil, time.Now)
	processor := scrapeservice.NewProcessor(repository, lookup, recorder)
	if err := scrapeservice.NewWorker(repository, processor, time.Now).Run(t.Context(), runID); err != nil {
		t.Fatal(err)
	}
	var attempts int
	var jobState, runState string
	err := dbapi.QueryRowContext(t.Context(), database, `SELECT j.state,r.state,
 (SELECT count(*) FROM metadata_scrape_query_attempts a WHERE a.scrape_run_id=r.id)
 FROM metadata_scrape_runs r JOIN jobs j ON j.id=r.job_id WHERE r.id=?`, runID).
		Scan(&jobState, &runState, &attempts)
	if err != nil || jobState != "SUCCEEDED" || runState != "COMPLETED" || attempts != want || lookup.calls != want {
		t.Fatalf("scrape attempts=%d calls=%d state=%s/%s error=%v", attempts, lookup.calls, jobState, runState, err)
	}
}

func assertEmptyAndMissingParentScrape(t *testing.T, database dbapi.DB, gameID string, version int64) {
	t.Helper()
	if _, err := database.ExecContext(t.Context(), "UPDATE dat_rom_entries SET status='NODUMP' WHERE machine_name='a'"); err != nil {
		t.Fatal(err)
	}
	scheduler := scrapeservice.NewScheduler(scrapepersistence.NewScheduler(database), nil, time.Now)
	empty, next, err := scheduler.ScheduleGame(t.Context(), gameID, version)
	if err != nil {
		t.Fatal(err)
	}
	runParentScrape(t, database, empty.RunID, 0)
	if _, err := database.ExecContext(t.Context(), `DELETE FROM archive_entries WHERE archive_file_record IN
 (SELECT file_record FROM game_files WHERE game_id=? AND role='CONTENT')`, gameID); err != nil {
		t.Fatal(err)
	}
	_, _, err = scheduler.ScheduleGame(t.Context(), gameID, next)
	if !errors.Is(err, scrapeservice.ErrArchiveIndexMissing) {
		t.Fatalf("missing archive index error = %v", err)
	}
	var currentVersion int64
	var runID string
	if err := dbapi.QueryRowContext(t.Context(), database,
		`SELECT g.version,r.id FROM games g JOIN metadata_scrape_runs r ON r.game_id=g.id WHERE g.id=?`, gameID).
		Scan(&currentVersion, &runID); err != nil {
		t.Fatal(err)
	}
	if currentVersion != next || runID != empty.RunID {
		t.Fatalf("failed scheduling changed version or replaced result: %d %s", currentVersion, runID)
	}
}
