package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/store"
	"retrom/internal/testassert"
)

func insertJob(t *testing.T, database *store.DB, id, kind, state string, retryable any, now int64) {
	t.Helper()
	digest := sha256.Sum256([]byte(id))
	finished := any(nil)
	if state == "FAILED" {
		finished = now
	}
	_, err := database.SQL.ExecContext(context.Background(),
		`
INSERT INTO jobs(id,
scope_type,
scope_id,
kind,
dedupe_key,
execution_no,
payload_json,
cancellable,
state,
attempt_count,
max_attempts,
version,
available_at_ms,
finished_at_ms,
error_code,
error_retryable,
created_at_ms,
updated_at_ms) VALUES(?,
'TEST',
'scope',
?,
?,
1,
'{}',
1,
?,
1,
2,
1,
?,
?,
?,
 ?,
?,
?)
`,
		id,
		kind,
		hex.EncodeToString(digest[:]),
		state,
		now,
		finished,
		"TEST_FAILURE",
		retryable,
		now,
		now,
	)
	testassert.False(t, err != nil, err)
}

func TestCancelAndRetryEnforceVersionedState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000)
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "retrom.db"), func() time.Time { return now })
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	service := New(database.SQL, func() time.Time { return now })
	insertJob(t, database, "cancel-job", "GAME_FILE_REVISION", "QUEUED", nil, now.UnixMilli())
	canceled, pending, err := service.Cancel(ctx, "cancel-job", 1, "operator canceled")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return pending }, func() bool { return canceled.State != "CANCELLED" }, func() bool { return canceled.Version != 2 }), "cancel = %#v, pending=%v, error=%v", canceled, pending, err)
	if _, _, err := service.Cancel(ctx, "cancel-job", 2, "again"); !errors.Is(err, ErrConflict) {
		t.Fatalf("terminal cancellation = %v", err)
	}
	insertJob(t, database, "failed-cancel-job", "GAME_FILE_REVISION", "FAILED", int64(1), now.UnixMilli())
	failedCanceled, pending, err := service.Cancel(ctx, "failed-cancel-job", 1, "discard retryable attachment")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return pending }, func() bool { return failedCanceled.State != "CANCELLED" }, func() bool { return failedCanceled.Version != 2 }), "failed cancel = %#v, pending=%v, error=%v", failedCanceled, pending, err)

	insertJob(t, database, "retry-job", "GAME_FILE_REVISION", "FAILED", int64(1), now.UnixMilli())
	retried, err := service.Retry(ctx, "retry-job", 1)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return retried.State != "QUEUED" }, func() bool { return retried.ExecutionNo != 2 }, func() bool { return retried.Version != 2 }), "retry = %#v, error=%v", retried, err)
	var snapshots int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*)
FROM job_input_snapshots
WHERE job_id='retry-job'
`).Scan(&snapshots); err != nil ||
		snapshots != 1 {
		t.Fatalf("input snapshots = %d, error=%v", snapshots, err)
	}
}

func TestMetadataScrapeRetryMustUseDomainAction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000)
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "retrom.db"), func() time.Time { return now })
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	insertJob(t, database, "scrape-job", "METADATA_SCRAPE", "FAILED", int64(1), now.UnixMilli())
	if _, err := New(database.SQL, func() time.Time { return now }).Retry(ctx, "scrape-job", 1); !errors.Is(
		err,
		ErrRetryViaDomain,
	) {
		t.Fatalf("retry error = %v", err)
	}
}
