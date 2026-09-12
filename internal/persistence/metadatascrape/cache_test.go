package metadatascrape

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/hasheous"
	"retrom/internal/testsupport"
)

func TestCacheExpiryBoundaryAndCancellation(t *testing.T) {
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	digest := strings.Repeat("a", 64)
	_, err = database.SQL.ExecContext(t.Context(), `INSERT INTO metadata_provider_responses
 (id,provider,request_digest,outcome,raw_payload_state,fetched_at_ms,expires_at_ms)
 VALUES('response','HASHEOUS',?,'MISS','NONE',100,200)`, digest)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.SQL.ExecContext(t.Context(), `INSERT INTO metadata_provider_cache
 (provider,request_digest,current_response_id,expires_at_ms,updated_at_ms)
 VALUES('HASHEOUS',?,'response',200,100)`, digest)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewCache(database.SQL)
	value, found, err := repository.Cached(t.Context(), digest, 199)
	if err != nil || !found || value.ID != "response" || value.Outcome != hasheous.OutcomeMiss || value.RawSHA256 != "" {
		t.Fatalf("valid cache: %+v found=%t error=%v", value, found, err)
	}
	_, found, err = repository.Cached(t.Context(), digest, 200)
	if err != nil || found {
		t.Fatalf("expired cache found=%t error=%v", found, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, found, err = repository.Cached(ctx, digest, 100)
	if !errors.Is(err, context.Canceled) || found {
		t.Fatalf("cancellation lost: found=%t error=%v", found, err)
	}
}
