package payloadrelease

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/testsupport"
)

func seedExpiredProvider(t *testing.T, fixture gcSchedulingFixture) {
	t.Helper()
	_, err := fixture.database.ExecContext(t.Context(), `INSERT INTO metadata_provider_responses(
id,provider,request_digest,http_status,outcome,raw_response_blob_id,raw_payload_state,fetched_at_ms,expires_at_ms)
VALUES('expiry-response','HASHEOUS',?,200,'HIT','manual-gc-blob','RETAINED',0,1)`, digest64("e"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.database.ExecContext(t.Context(), `INSERT INTO metadata_provider_cache(
provider,request_digest,current_response_id,expires_at_ms,updated_at_ms)
VALUES('HASHEOUS',?,'expiry-response',1,0)`, digest64("e"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestProviderExpirationRollsBackUnconfirmedRelease(t *testing.T) {
	t.Parallel()
	fixture := newGCSchedulingFixture(t)
	seedExpiredProvider(t, fixture)
	cause := errors.New("provider expiration affected-row failure")
	var hits atomic.Int64
	fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(strings.Join(strings.Fields(query), " "), "UPDATE metadata_provider_responses SET") &&
				gcBoundArgument(args, "expiry-response") {
				hits.Add(1)
				return failedSchedulingCount{Result: result, cause: cause}, nil
			}
			return result, nil
		},
	})
	service, err := New(fault, fixture.blobs, fixture.service.now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	count, err := service.releaseExpiredProviderPayloadBatch(t.Context())
	var state string
	var caches, candidates int
	readErr := fixture.database.QueryRowContext(t.Context(), `SELECT raw_payload_state,
(SELECT count(*) FROM metadata_provider_cache WHERE current_response_id='expiry-response'),
(SELECT count(*) FROM blob_gc_candidates WHERE blob_id='manual-gc-blob')
FROM metadata_provider_responses WHERE id='expiry-response'`).Scan(&state, &caches, &candidates)
	if !errors.Is(err, cause) || hits.Load() != 1 || count != 0 || readErr != nil ||
		state != "RETAINED" || caches != 1 || candidates != 0 {
		t.Fatalf("expiration retained partial writes: state=%s caches=%d candidates=%d count=%d hits=%d err=%v read=%v",
			state, caches, candidates, count, hits.Load(), err, readErr)
	}
}
