//go:build integration

package launch

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/integration/payloadrelease"
	"retrom/internal/testkit/testsupport"
)

type previewExpirationCountFailure struct {
	driver.Result
	cause error
}

func (result previewExpirationCountFailure) RowsAffected() (int64, error) { return 0, result.cause }

func TestPreviewExpirationRollsBackUnconfirmedRelease(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	preview := fixture.preview(t, "expiry-failure")
	if _, _, err := fixture.saver.CreateManual(t.Context(), preview.PreviewID, preview.Capability,
		"expiry-checkpoint", reviewCheckpointRequest(t, "retain-on-failure")); err != nil {
		t.Fatal(err)
	}
	var beforeState, checkpointID string
	var beforeVersion int64
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT state,version,checkpoint_payload_blob_id
FROM review_preview_sessions WHERE id=?`, preview.PreviewID).Scan(&beforeState, &beforeVersion, &checkpointID); err != nil {
		t.Fatal(err)
	}
	*fixture.now = fixture.now.Add(3 * time.Hour)
	cause := errors.New("preview expiry count failure")
	var hits atomic.Int64
	fault := newPreviewExpirationFault(t, fixture.database, preview.PreviewID, cause, &hits)
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	releaser, err := payloadrelease.New(fault, blobs, func() time.Time { return *fixture.now }, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer releaser.Close()
	err = releaser.ReconcileGC(t.Context())
	assertPreviewExpirationRollback(t, fixture.database, preview.PreviewID, checkpointID, beforeState, beforeVersion, cause, &hits, err)
}

func newPreviewExpirationFault(t *testing.T, database *sql.DB, previewID string, cause error, hits *atomic.Int64) *sql.DB {
	t.Helper()
	return testsupport.OpenSQLFaultDatabase(t, database, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(strings.Join(strings.Fields(query), " "), "UPDATE review_preview_sessions SET") &&
				strings.Contains(query, "checkpoint_payload_blob_id=NULL") {
				for _, arg := range args {
					if arg.Value == previewID {
						hits.Add(1)
						return previewExpirationCountFailure{Result: result, cause: cause}, nil
					}
				}
			}
			return result, nil
		},
	})
}

func assertPreviewExpirationRollback(t *testing.T, database *sql.DB, previewID, checkpointID, beforeState string, beforeVersion int64, cause error, hits *atomic.Int64, err error) {
	t.Helper()
	var state string
	var version int64
	var retained, candidates int
	readErr := database.QueryRowContext(t.Context(), `SELECT state,version,checkpoint_payload_blob_id IS NOT NULL,
(SELECT count(*) FROM blob_gc_candidates WHERE blob_id=?)
FROM review_preview_sessions WHERE id=?`, checkpointID, previewID).Scan(&state, &version, &retained, &candidates)
	if !errors.Is(err, cause) || hits.Load() != 1 || readErr != nil || state != beforeState || version != beforeVersion ||
		retained != 1 || candidates != 0 {
		t.Fatalf("preview expiry retained partial writes: state=%s version=%d payload=%d candidates=%d hits=%d err=%v read=%v",
			state, version, retained, candidates, hits.Load(), err, readErr)
	}
}
