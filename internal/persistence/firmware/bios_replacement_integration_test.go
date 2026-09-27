//go:build integration

package firmware

import (
	"context"
	"database/sql"
	"testing"

	dbapi "retrom/internal/database"

	"retrom/internal/composition/cleanupjobs"
	"retrom/internal/testassert"
)

func assertDeferredBIOSRelease(t *testing.T, ctx context.Context, database dbapi.DB,
	releases *cleanupjobs.Service, lifecycle firmwareReplacementLifecycle, fileRecord, installationID string,
) {
	t.Helper()
	finishFirmwareReleaseJobs(t, ctx, releases)
	testassert.False(t, releases.ReconcileDeletion(ctx) != nil, "reconcile retired BIOS")
	var oldBlob sql.NullString
	var released sql.NullInt64
	err := dbapi.QueryRowContext(ctx, database, `SELECT file_record,payload_released_at_ms FROM bios_installations WHERE id=?`, installationID).Scan(&oldBlob, &released)
	testassert.False(t, err != nil, err)
	testassert.True(t, !oldBlob.Valid && released.Valid, "retired installation still owns payload")
	var refs, candidates int
	err = dbapi.QueryRowContext(ctx, database, `SELECT
 (SELECT count(*) FROM launch_external_files WHERE launch_session_id=? AND file_record=?),
 (SELECT count(*) FROM job_input_snapshots WHERE json_extract(input_json,'$.inputs.relativePath')=?)`, lifecycle.launchID, fileRecord, "bios/"+installationID).Scan(&refs, &candidates)
	testassert.False(t, err != nil, err)
	testassert.Truef(t, refs == 0 && candidates == 1,
		"replaced BIOS remained unqueued: launch refs=%d, DeletionQueue candidates=%d", refs, candidates)
	var saves int
	err = dbapi.QueryRowContext(ctx, database, `SELECT count(*) FROM save_states WHERE id=?`, lifecycle.saveID).Scan(&saves)
	testassert.False(t, err != nil, err)
	testassert.True(t, saves == 1, "replacement or session expiry deleted save")
}

func finishFirmwareReleaseJobs(t *testing.T, ctx context.Context, releases *cleanupjobs.Service) {
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
