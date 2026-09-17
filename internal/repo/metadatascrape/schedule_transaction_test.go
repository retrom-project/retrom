package metadatascrape

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/service/metadatascrape"
	"retrom/internal/testkit/testsupport"
)

func TestMissingScrapeSubjectRollsBackCreatedJob(t *testing.T) {
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	now := int64(100)
	err = NewScheduler(database.SQL).CommitWrite(t.Context(), func(scope metadatascrape.ScheduleScope) error {
		return scope.Writes.Create(t.Context(), metadatascrape.SchedulePlan{
			Subject: metadatascrape.Subject{Kind: "IMPORT_ITEM", ID: "missing"}, RunID: "run", JobID: "job", Provider: "NONE",
			Dedupe: strings.Repeat("a", 64), PayloadJSON: `{"provider":"NONE"}`, JobState: "SUCCEEDED", RunState: "COMPLETED", EventJSON: "{}", FinishedAt: &now, Now: now,
		})
	})
	if err == nil {
		t.Fatal("scheduled nonexistent import item")
	}
	var jobs int
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 {
		t.Fatal("scrape scheduling committed a job without its run")
	}
}
