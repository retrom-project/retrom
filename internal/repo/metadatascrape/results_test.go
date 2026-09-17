package metadatascrape

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/adapter/metadata/hasheous"
	"retrom/internal/service/metadatascrape"
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
	err = NewRecorder(database.SQL).CommitWrite(t.Context(), func(scope metadatascrape.ResultScope) error {
		if err := scope.Write.Response(t.Context(), metadatascrape.ResponseRecord{
			ID: "response", RequestDigest: strings.Repeat("a", 64),
			Outcome: hasheous.OutcomeMiss, Cacheable: true, Now: 100, ExpiresAt: 200,
		}); err != nil {
			return err
		}
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
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
