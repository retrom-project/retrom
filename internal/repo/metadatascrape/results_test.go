package metadatascrape

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/adapter/metadata/hasheous"
	"retrom/internal/model/metadatascrape"
	"retrom/internal/testkit/testsupport"
)

func TestResponseAndCacheRollbackTogether(t *testing.T) {
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	repo := NewRecorder(database.SQL)
	WithResultPreCommitHook(repo, func() error {
		return context.Canceled
	})
	_, err = repo.CommitRecord(t.Context(), metadatascrape.RecordCommand{
		Attempt: metadatascrape.LookupAttempt{
			Claim: metadatascrape.WorkerClaim{RunID: "run", JobID: "job", WorkerID: "w", ExecutionNo: 1},
			Lookup: metadatascrape.ResolvedLookup{
				Result: hasheous.LookupResult{Outcome: hasheous.OutcomeMiss},
			},
		},
		Now: 100,
	})
	if !errors.Is(err, metadatascrape.ErrExecutionLost) && !errors.Is(err, context.Canceled) {
		t.Fatalf("late response failure: %v", err)
	}
	var responses, cache int
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM metadata_provider_responses),(SELECT count(*) FROM metadata_provider_cache)`).Scan(&responses, &cache); err != nil {
		t.Fatal(err)
	}
	if responses != 0 || cache != 0 {
		t.Fatalf("partial result committed: responses=%d cache=%d", responses, cache)
	}
}
