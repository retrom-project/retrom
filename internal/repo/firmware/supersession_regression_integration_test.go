//go:build integration

package firmware

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/model/firmware"
	"retrom/internal/repo/dbexec"
	firmwareservice "retrom/internal/service/firmware"
	"retrom/internal/testkit/testsupport"
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
			if err := db.QueryRowContext(t.Context(), `SELECT requirement_id FROM bios_installations
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
				dbexec.Rollback(tx)
			}
			var active, version int
			readErr := db.QueryRowContext(t.Context(), `SELECT is_active,version FROM bios_installations
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
			if err := db.QueryRowContext(t.Context(), `SELECT requirement_id FROM bios_installations WHERE id='read-failure-installation'`).Scan(&requirement); err != nil {
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
			err := testWithWrite(t.Context(), New(fault), func(scope firmware.WriteScope) error {
				return firmwareservice.SupersedeInScope(t.Context(), scope.Retirements, requirement, now)
			})
			var active, version int
			readErr := db.QueryRowContext(t.Context(), `SELECT is_active,version FROM bios_installations WHERE id='read-failure-installation'`).Scan(&active, &version)
			if !errors.Is(err, cause) || hits.Load() != 1 || readErr != nil || active != 1 || version != 1 {
				t.Fatalf("supersession read committed: active=%d version=%d hits=%d err=%v read=%v", active, version, hits.Load(), err, readErr)
			}
			assertBIOSReferenceCounts(t, db, 1, 1)
		})
	}
}
