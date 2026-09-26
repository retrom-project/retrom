//go:build integration

package firmware

import (
	"context"
	"database/sql"
	"testing"

	dbapi "retrom/internal/database"

	"retrom/internal/payloadrelease"
	"retrom/internal/testassert"
)

func assertDeferredBIOSRelease(t *testing.T, ctx context.Context, database dbapi.DB,
	releases *payloadrelease.Service, lifecycle firmwareReplacementLifecycle, blobID, installationID string,
) {
	t.Helper()
	finishFirmwareReleaseJobs(t, ctx, releases)
	testassert.False(t, releases.ReconcileGC(ctx) != nil, "reconcile retired BIOS")
	var oldBlob sql.NullString
	var released sql.NullInt64
	err := dbapi.QueryRowContext(ctx, database, `SELECT blob_id,payload_released_at_ms FROM bios_installations WHERE id=?`, installationID).Scan(&oldBlob, &released)
	testassert.False(t, err != nil, err)
	testassert.True(t, !oldBlob.Valid && released.Valid, "retired installation still owns payload")
	var refs, candidates, blobs int
	err = dbapi.QueryRowContext(ctx, database, `SELECT
 (SELECT count(*) FROM launch_external_files WHERE launch_session_id=? AND blob_id=?),
 (SELECT count(*) FROM blob_gc_candidates WHERE blob_id=?),
 (SELECT count(*) FROM blobs WHERE id=?)`, lifecycle.launchID, blobID, blobID, blobID).Scan(&refs, &candidates, &blobs)
	testassert.False(t, err != nil, err)
	testassert.Truef(t, refs == 0 && (blobs == 0 || candidates == 1),
		"replaced BIOS remained unqueued: launch refs=%d, GC candidates=%d, blobs=%d", refs, candidates, blobs)
	var saves int
	err = dbapi.QueryRowContext(ctx, database, `SELECT count(*) FROM save_states WHERE id=?`, lifecycle.saveID).Scan(&saves)
	testassert.False(t, err != nil, err)
	testassert.True(t, saves == 1, "replacement or session expiry deleted save")
}

func finishFirmwareReleaseJobs(t *testing.T, ctx context.Context, releases *payloadrelease.Service) {
	t.Helper()
	// Replacement queues upload consumption release. Complete that work explicitly
	// before asserting that the launch is the old BIOS payload's final owner.
	for range 100 {
		worked, err := releases.RunOnce(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			return
		}
	}
	t.Fatal("firmware release jobs did not drain")
}
