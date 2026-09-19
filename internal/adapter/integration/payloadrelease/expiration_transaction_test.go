package payloadrelease

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	payloadreleasemodel "retrom/internal/model/payloadrelease"
	"retrom/internal/repo/dbexec"
	repository "retrom/internal/repo/payloadrelease"
	"retrom/internal/testkit/testsupport"
)

func TestProviderExpirationFailuresRollbackCacheResponseAndGC(t *testing.T) {
	t.Parallel()
	for _, point := range []string{"read", "cache SQL", "cache count", "cache zero", "response zero", "GC SQL"} {
		t.Run(point, func(t *testing.T) {
			t.Parallel()
			fixture := newGCSchedulingFixture(t)
			seedExpiredProvider(t, fixture)
			cause := errors.New(point + " failure")
			var hits atomic.Int64
			fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, expirationFault(point, cause, &hits))
			service, err := New(fault, fixture.blobs, fixture.service.now, 24*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close()
			count, err := service.releaseExpiredProviderPayloadBatch(t.Context())
			want := cause
			if strings.HasSuffix(point, "zero") {
				want = payloadreleasemodel.ErrExpirationSnapshotChanged
			}
			if !errors.Is(err, want) || count != 0 || hits.Load() != 1 {
				t.Fatalf("wrong expiry failure: count=%d hits=%d err=%v", count, hits.Load(), err)
			}
			assertProviderExpirationUnchanged(t, fixture)
		})
	}
}

func expirationFault(point string, cause error, hits *atomic.Int64) testsupport.SQLFaultHooks {
	return testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if point == "read" && strings.Contains(query, "FROM metadata_provider_responses WHERE raw_payload_state=") {
				hits.Add(1)
				return cause
			}
			return nil
		},
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			query = strings.Join(strings.Fields(query), " ")
			match := strings.HasPrefix(point, "cache") &&
				strings.HasPrefix(query, "DELETE FROM metadata_provider_cache") && gcBoundArgument(args, "expiry-response") ||
				point == "response zero" && strings.HasPrefix(query, "UPDATE metadata_provider_responses SET") &&
					gcBoundArgument(args, "expiry-response") || point == "GC SQL" &&
				strings.HasPrefix(query, "INSERT INTO jobs") && gcBoundArgument(args, "manual-gc-blob")
			if !match {
				return result, nil
			}
			hits.Add(1)
			if strings.HasSuffix(point, "SQL") {
				return nil, cause
			}
			if strings.HasSuffix(point, "zero") {
				cause = nil
			}
			return failedSchedulingCount{Result: result, cause: cause}, nil
		},
	}
}

func assertProviderExpirationUnchanged(t *testing.T, fixture gcSchedulingFixture) {
	t.Helper()
	var state, blobID string
	var caches, candidates, jobs int
	err := fixture.database.QueryRowContext(t.Context(), `SELECT raw_payload_state,raw_response_blob_id,
(SELECT count(*) FROM metadata_provider_cache WHERE current_response_id='expiry-response'),
(SELECT count(*) FROM blob_gc_candidates WHERE blob_id='manual-gc-blob'),
(SELECT count(*) FROM jobs WHERE scope_type='BLOB' AND scope_id='manual-gc-blob')
FROM metadata_provider_responses WHERE id='expiry-response'`).Scan(&state, &blobID, &caches, &candidates, &jobs)
	if err != nil || state != "RETAINED" || blobID != "manual-gc-blob" || caches != 1 || candidates != 0 || jobs != 0 {
		t.Fatalf("expiration leaked partial writes: state=%s blob=%s caches=%d candidates=%d jobs=%d err=%v",
			state, blobID, caches, candidates, jobs, err)
	}
}

func TestProviderExpirationRepositoryRejectsRenewedExpiry(t *testing.T) {
	t.Parallel()
	fixture := newGCSchedulingFixture(t)
	seedExpiredProvider(t, fixture)
	repo := repository.NewExpiration(fixture.database)
	var before payloadreleasemodel.ProviderExpiration
	err := repo.WithExpiration(t.Context(), func(scope payloadreleasemodel.ExpirationScope) error {
		facts, err := scope.Read.Providers(t.Context(), 10, 200)
		if err != nil {
			return err
		}
		if len(facts) != 1 {
			return fmt.Errorf("expiry facts count = %d", len(facts))
		}
		before = facts[0]
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE metadata_provider_responses SET expires_at_ms=11
WHERE id='expiry-response'`); err != nil {
		t.Fatal(err)
	}
	err = repo.WithExpiration(t.Context(), func(scope payloadreleasemodel.ExpirationScope) error {
		return scope.Write.ReleaseProvider(t.Context(), before, 10)
	})
	if !errors.Is(err, payloadreleasemodel.ErrExpirationSnapshotChanged) {
		t.Fatalf("renewed response released: %v", err)
	}
	assertProviderExpirationUnchanged(t, fixture)
}

func TestProviderExpirationDrainsBoundedBatches(t *testing.T) {
	t.Parallel()
	fixture := newGCSchedulingFixture(t)
	seedExpiredProvider(t, fixture)
	tx, err := fixture.database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	for index := range 200 {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO metadata_provider_responses(
id,provider,request_digest,http_status,outcome,raw_response_blob_id,raw_payload_state,fetched_at_ms,expires_at_ms)
VALUES(?,'HASHEOUS',?,200,'HIT','manual-gc-blob','RETAINED',0,1)`, fmt.Sprintf("expiry-%03d", index), digest64("e"))
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	count, err := fixture.service.releaseExpiredProviderPayloadBatch(t.Context())
	if err != nil || count != 200 {
		t.Fatalf("bounded first batch: count=%d err=%v", count, err)
	}
	if err := fixture.service.releaseExpiredProviderPayloads(t.Context()); err != nil {
		t.Fatal(err)
	}
	var remaining, candidates int
	err = fixture.database.QueryRowContext(t.Context(), `SELECT
(SELECT count(*) FROM metadata_provider_responses WHERE raw_payload_state='RETAINED'),
(SELECT count(*) FROM blob_gc_candidates WHERE blob_id='manual-gc-blob')`).Scan(&remaining, &candidates)
	if err != nil || remaining != 0 || candidates != 1 {
		t.Fatalf("expiration did not drain with one shared GC candidate: remaining=%d candidates=%d err=%v",
			remaining, candidates, err)
	}
}
