//go:build integration

package firmware

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	dbapi "retrom/internal/database"
	firmwareservice "retrom/internal/service/firmware"
	"retrom/internal/testsupport"
)

func TestBIOSSupersessionRejectsUnconfirmedDeactivate(t *testing.T) {
	t.Parallel()
	for _, zero := range []bool{false, true} {
		t.Run(map[bool]string{true: "zero", false: "count failure"}[zero], func(t *testing.T) {
			t.Parallel()
			db, releases, now := retirementFixture(t)
			t.Cleanup(releases.Close)
			seedRetiringInstallation(t, db, "superseded-installation", 1, now)
			var requirement string
			if err := dbapi.QueryRowContext(t.Context(), db, `SELECT requirement_id FROM bios_installations
WHERE id='superseded-installation'`).Scan(&requirement); err != nil {
				t.Fatal(err)
			}
			cause := errors.New("BIOS deactivate count failure")
			if zero {
				cause = nil
			}
			var hits atomic.Int64
			fault := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
				AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
					query = strings.Join(strings.Fields(query), " ")
					if strings.HasPrefix(query, "UPDATE bios_installations SET") && strings.Contains(query, "is_active=0") {
						for _, arg := range args {
							if arg.Value == "superseded-installation" {
								hits.Add(1)
								return retirementFailedCount{Result: result, cause: cause}, nil
							}
						}
					}
					return result, nil
				},
			})
			tx, err := fault.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			err = firmwareservice.SupersedeInScope(t.Context(), BindSupersession(tx), requirement, now)
			if err == nil {
				err = tx.Commit()
			} else {
				dbapi.Rollback(tx)
			}
			var active, version int
			readErr := dbapi.QueryRowContext(t.Context(), db, `SELECT is_active,version FROM bios_installations
WHERE id='superseded-installation'`).Scan(&active, &version)
			if err == nil || cause != nil && !errors.Is(err, cause) || hits.Load() != 1 || readErr != nil || active != 1 || version != 1 {
				t.Fatalf("unconfirmed supersession committed: active=%d version=%d hits=%d err=%v read=%v",
					active, version, hits.Load(), err, readErr)
			}
			assertBIOSReferenceCounts(t, db, 1, 1)
		})
	}
}

func TestBIOSSupersessionReadFailuresRollBackCurrentInstallation(t *testing.T) {
	t.Parallel()
	for _, fragment := range []string{"SELECT id,requirement_id,blob_id,version", "SELECT id FROM upload_consumptions"} {
		t.Run(fragment, func(t *testing.T) {
			t.Parallel()
			db, releases, now := retirementFixture(t)
			t.Cleanup(releases.Close)
			seedRetiringInstallation(t, db, "read-failure-installation", 1, now)
			var requirement string
			if err := dbapi.QueryRowContext(t.Context(), db, `SELECT requirement_id FROM bios_installations WHERE id='read-failure-installation'`).Scan(&requirement); err != nil {
				t.Fatal(err)
			}
			cause := errors.New("supersession read failure")
			var hits atomic.Int64
			fault := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
				BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
					if strings.HasPrefix(strings.Join(strings.Fields(query), " "), fragment) {
						hits.Add(1)
						return cause
					}
					return nil
				},
			})
			err := New(fault).WithWrite(t.Context(), func(scope firmwareservice.WriteScope) error {
				return firmwareservice.SupersedeInScope(t.Context(), scope.Retirements, requirement, now)
			})
			var active, version int
			readErr := dbapi.QueryRowContext(t.Context(), db, `SELECT is_active,version FROM bios_installations WHERE id='read-failure-installation'`).Scan(&active, &version)
			if !errors.Is(err, cause) || hits.Load() != 1 || readErr != nil || active != 1 || version != 1 {
				t.Fatalf("supersession read committed: active=%d version=%d hits=%d err=%v read=%v", active, version, hits.Load(), err, readErr)
			}
			assertBIOSReferenceCounts(t, db, 1, 1)
		})
	}
}

func TestBIOSSupersessionOnlyRevokesLaunchesUsingReplacedInstallation(t *testing.T) {
	db, releases, now := retirementFixture(t)
	t.Cleanup(releases.Close)
	var blobID string
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT blob_id FROM launch_external_files
WHERE launch_session_id='firmware-launch' AND kind='BIOS_BUNDLE'`).Scan(&blobID); err != nil {
		t.Fatal(err)
	}
	_, err := db.ExecContext(t.Context(), `INSERT INTO launch_sessions(
id,profile_id,game_id,core_id,provider_id,target_id,bundle_sha256,content_kind,
dependency_snapshot_json,compatibility_code,return_to,credential_sha256,state,
bootstrap_expires_at_ms,idle_expires_at_ms,activated_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms)
SELECT 'unrelated-launch',profile_id,game_id,core_id,provider_id,target_id,bundle_sha256,content_kind,
replace(dependency_snapshot_json,'retirement-installation','other-installation'),
compatibility_code,return_to,credential_sha256,state,
bootstrap_expires_at_ms,idle_expires_at_ms,activated_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms
FROM launch_sessions WHERE id='firmware-launch'`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(t.Context(), `INSERT INTO launch_external_files(
launch_session_id,virtual_path,logical_name,blob_id,created_at_ms,kind)
SELECT 'unrelated-launch',virtual_path,logical_name,blob_id,created_at_ms,kind
FROM launch_external_files WHERE launch_session_id='firmware-launch'`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(tx)
	if err := (supersessionRecords{executor: tx}).revokeBIOSLaunches(
		t.Context(), "retirement-installation", blobID, now,
	); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var affected, unrelated string
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT
(SELECT state FROM launch_sessions WHERE id='firmware-launch'),
(SELECT state FROM launch_sessions WHERE id='unrelated-launch')`).Scan(&affected, &unrelated); err != nil {
		t.Fatal(err)
	}
	if affected != "REVOKED" || unrelated != "ACTIVE" {
		t.Fatalf("shared BIOS supersession affected %s/%s", affected, unrelated)
	}
}
