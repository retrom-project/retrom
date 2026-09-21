package metadatascrape

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"retrom/internal/testsupport"
)

func TestCandidateMediaJobAndFrozenInputRollbackWithResult(t *testing.T) {
	fixture := newEmptyMediaFixture(t)
	cause := errors.New("media snapshot storage unavailable")
	hits := 0
	faulted := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if !strings.Contains(query, "INSERT INTO job_input_snapshots") || len(args) != 4 {
				return nil
			}
			input, ok := args[1].Value.(string)
			if ok && strings.Contains(input, `"kind":"MEDIA_FETCH"`) && strings.Contains(input, `"id":"game"`) {
				hits++
				return cause
			}
			return nil
		},
	})
	err := fixture.record(t.Context(), NewRecorder(faulted))
	if !errors.Is(err, cause) || hits != 1 {
		t.Fatalf("candidate did not atomically freeze media job input: cause=%v hits=%d", err, hits)
	}
	var responses, candidates, assets, jobs, inputs, budgets int
	err = fixture.database.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM metadata_provider_responses),
 (SELECT count(*) FROM scrape_candidates),(SELECT count(*) FROM scrape_candidate_assets),
 (SELECT count(*) FROM jobs WHERE kind='MEDIA_FETCH'),(SELECT count(*) FROM job_input_snapshots),
 (SELECT count(*) FROM metadata_media_runs)`).Scan(&responses, &candidates, &assets, &jobs, &inputs, &budgets)
	if err != nil {
		t.Fatal(err)
	}
	if responses+candidates+assets+jobs+budgets != 0 || inputs != 0 {
		t.Fatalf("partial media result: responses=%d candidates=%d assets=%d jobs=%d inputs=%d budgets=%d",
			responses, candidates, assets, jobs, inputs, budgets)
	}
}
