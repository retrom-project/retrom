//go:build integration

package firmware

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
	"retrom/internal/persistence/recordstore"
	"retrom/internal/testkit/testsupport"
)

type retirementFailedCount struct {
	driver.Result
	cause error
}

func (result retirementFailedCount) RowsAffected() (int64, error) { return 0, result.cause }

func faultRetirementService(t *testing.T, db *sql.DB, now int64, prefix, fragment string,
	cause error, hits *atomic.Int64,
) *payloadrelease.Service {
	t.Helper()
	fault := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, _ []driver.NamedValue, result driver.Result) (driver.Result, error) {
			query = strings.Join(strings.Fields(query), " ")
			if strings.HasPrefix(query, prefix) && strings.Contains(query, fragment) {
				hits.Add(1)
				return retirementFailedCount{Result: result, cause: cause}, nil
			}
			return result, nil
		},
	})
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service, err := payloadrelease.New(fault, blobs, func() time.Time { return time.UnixMilli(now) }, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return service
}

func TestBIOSRetirementRollsBackUnconfirmedInstallationRelease(t *testing.T) {
	t.Parallel()
	db, original, now := retirementFixture(t)
	t.Cleanup(original.Close)
	seedRetiringInstallation(t, db, "expiry-installation", 0, now)
	cause := errors.New("BIOS retirement count failure")
	var hits atomic.Int64
	service := faultRetirementService(t, db, now, "UPDATE bios_installations SET", "blob_id=NULL", cause, &hits)
	err := service.ReconcileGC(t.Context())
	var retained, variants int
	readErr := db.QueryRowContext(t.Context(), `SELECT blob_id IS NOT NULL,
(SELECT count(*) FROM variant_files WHERE game_variant_id='firmware-variant')
FROM bios_installations WHERE id='expiry-installation'`).Scan(&retained, &variants)
	if !errors.Is(err, cause) || hits.Load() != 1 || readErr != nil || retained != 1 || variants != 1 {
		t.Fatalf("unconfirmed BIOS retirement committed: retained=%d variants=%d hits=%d err=%v read=%v",
			retained, variants, hits.Load(), err, readErr)
	}
}

func TestLaunchRetirementRollsBackUnconfirmedWrites(t *testing.T) {
	t.Parallel()
	for _, point := range []struct{ name, prefix, fragment string }{
		{"launch", "UPDATE launch_sessions SET", "finished_at_ms="},
		{"play", "UPDATE play_sessions SET", "ended_at_ms="},
		{"external", "DELETE FROM launch_external_files", "WHERE"},
		{"content", "DELETE FROM launch_content_files", "WHERE"},
		{"retirement", "UPDATE launch_payload_retirements SET released_at_ms=", "WHERE"},
	} {
		t.Run(point.name, func(t *testing.T) {
			t.Parallel()
			db, original, now := retirementFixture(t)
			t.Cleanup(original.Close)
			seedExpiringFirmwarePlay(t, db, now)
			cause := errors.New(point.name + " retirement count failure")
			var hits atomic.Int64
			service := faultRetirementService(t, db, now, point.prefix, point.fragment, cause, &hits)
			err := service.ReconcileGC(t.Context())
			if !errors.Is(err, cause) || hits.Load() != 1 {
				t.Fatalf("unconfirmed retirement: hits=%d err=%v", hits.Load(), err)
			}
			assertFirmwareRetirementUnchanged(t, db)
		})
	}
}

func seedExpiringFirmwarePlay(t *testing.T, db *sql.DB, now int64) {
	t.Helper()
	_, err := updateFirmwareLaunch(t, db, recordstore.Update{
		Set:   `idle_expires_at_ms=?,updated_at_ms=?,version=version+1`,
		Scope: recordstore.Scope{Where: `id='firmware-launch'`}, Values: []any{now, now},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(t.Context(), `INSERT INTO play_sessions(
id,launch_session_id,profile_id,game_id,started_at_ms,last_heartbeat_at_ms,state,created_at_ms,updated_at_ms)
VALUES('expiry-play','firmware-launch','firmware-profile','firmware-game',?,?,'ACTIVE',?,?)`, now, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
}

func assertFirmwareRetirementUnchanged(t *testing.T, db *sql.DB) {
	t.Helper()
	var launch, play string
	var content, external, released, saves int
	err := db.QueryRowContext(t.Context(), `SELECT
(SELECT state FROM launch_sessions WHERE id='firmware-launch'),
(SELECT state FROM play_sessions WHERE id='expiry-play'),
(SELECT count(*) FROM launch_content_files WHERE launch_session_id='firmware-launch'),
(SELECT count(*) FROM launch_external_files WHERE launch_session_id='firmware-launch'),
(SELECT count(*) FROM launch_payload_retirements WHERE launch_session_id='firmware-launch' AND released_at_ms IS NOT NULL),
(SELECT count(*) FROM save_states WHERE id='firmware-save')`).Scan(&launch, &play, &content, &external, &released, &saves)
	if err != nil || launch != "ACTIVE" || play != "ACTIVE" || content != 1 || external != 1 || released != 0 || saves != 1 {
		t.Fatalf("retirement changed inputs: launch=%s play=%s content=%d external=%d released=%d saves=%d err=%v",
			launch, play, content, external, released, saves, err)
	}
}
