//go:build integration

package firmware

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"retrom/internal/payloadrelease"
	"retrom/internal/testassert"
)

func assertDeferredBIOSRelease(t *testing.T, ctx context.Context, database *sql.DB,
	releases *payloadrelease.Service, lifecycle firmwareReplacementLifecycle, blobID, installationID string,
) {
	t.Helper()
	testassert.False(t, releases.ReconcileGC(ctx) != nil, "reconcile retired BIOS")
	var oldBlob sql.NullString
	var released sql.NullInt64
	err := database.QueryRowContext(ctx, `SELECT blob_id,payload_released_at_ms FROM bios_installations WHERE id=?`, installationID).Scan(&oldBlob, &released)
	testassert.False(t, err != nil, err)
	testassert.True(t, !oldBlob.Valid && released.Valid, "retired installation still owns payload")
	var refs, candidates int
	err = database.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM launch_external_files WHERE launch_session_id=? AND blob_id=?),
 (SELECT count(*) FROM blob_gc_candidates WHERE blob_id=?)`, lifecycle.launchID, blobID, blobID).Scan(&refs, &candidates)
	testassert.False(t, err != nil, err)
	testassert.True(t, refs == 1 && candidates == 0, "running session must protect old BIOS")
	now := time.Now().UnixMilli()
	_, err = database.ExecContext(ctx, `UPDATE launch_sessions SET state='FINISHED',finished_at_ms=?,updated_at_ms=?,version=version+1 WHERE id=?`, now, now, lifecycle.launchID)
	testassert.False(t, err != nil, err)
	testassert.False(t, releases.ReconcileGC(ctx) != nil, "reconcile finished launch")
	err = database.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM launch_external_files WHERE launch_session_id=?),
 (SELECT count(*) FROM blob_gc_candidates WHERE blob_id=?)`, lifecycle.launchID, blobID).Scan(&refs, &candidates)
	testassert.False(t, err != nil, err)
	testassert.True(t, refs == 0 && candidates == 1, "finished launch must release old BIOS")
	var saves int
	err = database.QueryRowContext(ctx, `SELECT count(*) FROM save_states WHERE id=?`, lifecycle.saveID).Scan(&saves)
	testassert.False(t, err != nil, err)
	testassert.True(t, saves == 1, "replacement or session expiry deleted save")
}
