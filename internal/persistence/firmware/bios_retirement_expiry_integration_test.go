//go:build integration

package firmware

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	dependencypersistence "retrom/internal/persistence/dependencies"
	dependencyservice "retrom/internal/service/dependencies"

	"retrom/internal/persistence/recordstore"

	"retrom/internal/cleanup"
	"retrom/internal/composition/cleanupjobs"
	"retrom/internal/dependencies"
	"retrom/internal/filestore"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

func TestBIOSLaunchRetirementDeadlines(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, state           string
		idle, hard, bootstrap int64
		expire                bool
	}{
		{"live", "ACTIVE", 1000, 2000, 1000, false},
		{"idle expired", "ACTIVE", 0, 2000, 1000, false},
		{"hard expired", "ACTIVE", 1000, 0, 0, true},
		{"unused bootstrap expired", "CREATED", 1000, 2000, 0, true},
		{"bootstrap still usable", "CREATED", 1000, 2000, 1000, false},
		{"finished", "FINISHED", 1000, 2000, 1000, true},
		{"revoked", "REVOKED", 1000, 2000, 1000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, releases, now := retirementFixture(t)
			var finish any
			if tc.state == "FINISHED" || tc.state == "REVOKED" {
				finish = now
			}
			_, err := updateFirmwareLaunch(t, database, recordstore.Update{Set: `state=?,finished_at_ms=?,
idle_expires_at_ms=?,hard_expires_at_ms=?,bootstrap_expires_at_ms=?,updated_at_ms=?,version=version+1`, Scope: recordstore.Scope{Where: `id='firmware-launch'`}, Values: []any{tc.state, finish, now + tc.idle, now + tc.hard, now + tc.bootstrap, now}})
			testassert.False(t, err != nil, err)
			testassert.False(t, releases.ReconcileDeletion(t.Context()) != nil, "reconcile")
			testassert.False(t, releases.ReconcileDeletion(t.Context()) != nil, "reconcile twice")
			var files, saves int
			var released sql.NullInt64
			err = dbapi.QueryRowContext(t.Context(), database, `SELECT
(SELECT count(*) FROM launch_external_files WHERE launch_session_id='firmware-launch'),
(SELECT count(*) FROM save_states WHERE id='firmware-save'),released_at_ms
FROM launch_payload_retirements WHERE launch_session_id='firmware-launch'`).Scan(&files, &saves, &released)
			testassert.False(t, err != nil, err)
			testassert.True(t, released.Valid == tc.expire && (files == 0) == tc.expire && saves == 1, "expiry touched live input or save")
		})
	}
}

func retirementFixture(t *testing.T) (dbapi.DB, *cleanupjobs.Service, int64) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	deps, err := dependencies.Load(filepath.Join("..", "..", "..", "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	testassert.False(t, dependencyservice.New(deps, dependencypersistence.New(database.SQL)).Bootstrap(ctx, time.Now()) != nil, "bootstrap")
	identity, err := testsupport.LookupRuntimeTarget(ctx, database.SQL, "mgba")
	testassert.False(t, err != nil, err)
	blobs, err := filestore.Open(dir)
	testassert.False(t, err != nil, err)
	bios := ensureFirmwareBlob(t, ctx, database.SQL, blobs, []byte("retirement BIOS"))
	seedFirmwareReplacementLifecycle(t, ctx, database.SQL, blobs, identity, "retirement-installation", bios)
	now := time.Now().Add(time.Second)
	releases, err := cleanupjobs.New(t.Context(), database.SQL, blobs, func() time.Time { return now })
	testassert.False(t, err != nil, err)
	return database.SQL, releases, now.UnixMilli()
}
