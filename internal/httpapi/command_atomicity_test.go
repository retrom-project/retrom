package httpapi

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	dbapi "retrom/internal/database"
	"retrom/internal/service/idempotency"
	"retrom/internal/testsupport"
)

func TestCommandReceiptOrCommitFailureRollsBackMutation(t *testing.T) {
	for _, phase := range []string{"receipt", "commit"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newCommandPeers(t)
			healthy := fixture.database
			cause := errors.New("command " + phase + " unavailable")
			if phase == "receipt" {
				fixture.database = testsupport.OpenSQLFaultDatabase(t, healthy, testsupport.SQLFaultHooks{
					BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
						if strings.Contains(query, "INSERT INTO idempotency_records") {
							return cause
						}
						return nil
					},
				})
			} else {
				fixture.database = commandCommitFailure{DB: healthy, cause: cause}
			}
			key := uuid.NewString()
			failed := fixture.send(t.Context(), 0, key)
			if failed.Code != 500 {
				t.Fatalf("failed command=%d/%s", failed.Code, failed.Body.String())
			}
			assertCommandCounts(t, healthy, 0, 0)
			fixture.database = healthy
			retry := fixture.send(t.Context(), 0, key)
			if retry.Code != 201 {
				t.Fatalf("retry=%d/%s", retry.Code, retry.Body.String())
			}
			assertCommandCounts(t, healthy, 1, 1)
			assertCommandReplay(t, retry, fixture.send(t.Context(), 1, key))
			assertCommandCounts(t, healthy, 1, 1)
		})
	}
}

func assertCommandCounts(t *testing.T, database dbapi.DB, writes, receipts int) {
	t.Helper()
	var actualWrites, actualReceipts int
	err := dbapi.QueryRowContext(t.Context(), database, `SELECT
 (SELECT count(*) FROM audit_events WHERE action='PROBE'),(SELECT count(*) FROM idempotency_records)`).Scan(&actualWrites, &actualReceipts)
	if err != nil {
		t.Fatal(err)
	}
	if actualWrites != writes || actualReceipts != receipts {
		t.Fatalf("writes/receipts=%d/%d, want %d/%d", actualWrites, actualReceipts, writes, receipts)
	}
}

type commandCommitFailure struct {
	dbapi.DB
	cause error
}

func (database commandCommitFailure) BeginTx(ctx context.Context, options *dbapi.TxOptions) (dbapi.Tx, error) {
	tx, err := database.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("begin injected commit: %w", err)
	}
	return commandFailedCommit{Tx: tx, cause: database.cause}, nil
}

type commandFailedCommit struct {
	dbapi.Tx
	cause error
}

func (tx commandFailedCommit) Commit() error {
	// Roll back the real SQL unit before simulating a failed COMMIT. This drives
	// the same participant outcome without relying on a process-global driver.
	return fmt.Errorf("injected commit failure: %w", errors.Join(tx.cause, tx.Rollback()))
}

func TestCommandExpiryAllowsNewMutationWithSameKey(t *testing.T) {
	fixture := newCommandPeers(t)
	var clock atomic.Int64
	clock.Store(time.Now().UnixMilli())
	for _, server := range fixture.servers {
		server.now = func() time.Time { return time.UnixMilli(clock.Load()) }
	}
	key := uuid.NewString()
	first := fixture.send(t.Context(), 0, key)
	assertCommandReplay(t, first, fixture.send(t.Context(), 1, key))
	clock.Add((24 * time.Hour).Milliseconds())
	next := fixture.send(t.Context(), 1, key)
	if next.Code != 201 || next.Body.String() == first.Body.String() || next.Header().Get("X-Retrom-Idempotent-Replay") != "" {
		t.Fatalf("expired key was replayed: %d/%s", next.Code, next.Body.String())
	}
	assertCommandCounts(t, fixture.database, 2, 1)
}

func TestCommandNoContentReceiptIsCommittedAndReplayable(t *testing.T) {
	fixture := newCommandPeers(t)
	key := uuid.NewString()
	send := func(index int) *httptest.ResponseRecorder {
		request := sourceCommandRequest(t.Context(), "/tags/one", "deleteAdminTag", key)
		request.Method = http.MethodDelete
		response := httptest.NewRecorder()
		fixture.handler(index).ServeHTTP(response, request)
		return response
	}
	first, replay := send(0), send(1)
	if first.Code != 204 || replay.Code != 204 || first.Body.Len() != 0 || replay.Body.Len() != 0 ||
		replay.Header().Get("ETag") != `"v1"` || replay.Header().Get("X-Retrom-Idempotent-Replay") != "true" {
		t.Fatalf("no content result: %d/%d %s/%s", first.Code, replay.Code, first.Body.String(), replay.Body.String())
	}
	assertCommandCounts(t, fixture.database, 1, 1)
}

func TestCommandLateReceiptConflictRollsBackAndPreservesOriginalResponse(t *testing.T) {
	for _, differentDigest := range []bool{false, true} {
		t.Run(fmt.Sprintf("differentDigest=%t", differentDigest), func(t *testing.T) {
			fixture := newCommandPeers(t)
			key := uuid.NewString()
			originalBody := []byte("original frozen response")
			fixture.before = func(ctx context.Context) {
				identity := idempotency.RequestFromContext(ctx)
				digest := identity.Digest
				if differentDigest {
					digest = strings.Repeat("f", 64)
				}
				now := time.Now().UnixMilli()
				if _, err := fixture.database.ExecContext(ctx, `INSERT INTO idempotency_records
(principal_id,operation_id,key,request_digest,http_status,response_headers_json,response_body,created_at_ms,expires_at_ms)
VALUES(?,?,?,?,201,'{}',?,?,?)`, identity.PrincipalID, identity.OperationID, identity.Key,
					digest, originalBody, now, now+(24*time.Hour).Milliseconds()); err != nil {
					t.Fatal(err)
				}
			}
			response := fixture.send(t.Context(), 0, key)
			expected := http.StatusInternalServerError
			if differentDigest {
				expected = http.StatusConflict
			}
			if response.Code != expected {
				t.Fatalf("late conflict=%d/%s", response.Code, response.Body.String())
			}
			assertCommandCounts(t, fixture.database, 0, 1)
			fixture.before = nil
			replay := fixture.send(t.Context(), 1, key)
			if differentDigest {
				if replay.Code != 409 || !strings.Contains(replay.Body.String(), "IDEMPOTENCY_KEY_REUSED") {
					t.Fatalf("conflicting input changed: %d/%s", replay.Code, replay.Body.String())
				}
			} else if replay.Code != 201 || replay.Body.String() != string(originalBody) ||
				replay.Header().Get("X-Retrom-Idempotent-Replay") != "true" {
				t.Fatalf("original completion changed: %d/%s", replay.Code, replay.Body.String())
			}
			assertCommandCounts(t, fixture.database, 0, 1)
		})
	}
}
