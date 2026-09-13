//go:build integration

package firmware

import (
	"errors"
	"sync/atomic"
	"testing"

	retirement "retrom/internal/persistence/payloadrelease"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/payloadrelease"
)

func TestRetirementRejectsReactivatedBIOS(t *testing.T) {
	t.Parallel()
	db, releases, now := retirementFixture(t)
	t.Cleanup(releases.Close)
	seedRetiringInstallation(t, db, "returning-installation", 0, now)
	repo := retirement.NewRetirement(db)
	var before application.BIOSRetirement
	err := repo.WithRetirement(t.Context(), func(scope application.RetirementScope) error {
		var err error
		before, err = scope.Read.BIOS(t.Context(), 200)
		return err
	})
	if err != nil || !before.Found {
		t.Fatalf("read retiring BIOS: %+v %v", before, err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE bios_installations SET is_active=1,version=version+1
WHERE id='returning-installation'`); err != nil {
		t.Fatal(err)
	}
	err = repo.WithRetirement(t.Context(), func(scope application.RetirementScope) error {
		if err := scope.BIOS.FenceBIOS(t.Context(), before); err != nil {
			return err
		}
		return scope.BIOS.ReleaseBIOSFiles(t.Context(), before)
	})
	if !errors.Is(err, application.ErrRetirementSnapshotChanged) {
		t.Fatalf("reactivated BIOS accepted: %v", err)
	}
	assertBIOSReferenceCounts(t, db, 1, 1)
}

func TestRetirementRejectsRenewedLaunchAndIncompleteFileDrain(t *testing.T) {
	t.Parallel()
	for _, renew := range []bool{false, true} {
		t.Run(map[bool]string{true: "renewed launch", false: "new frozen file"}[renew], func(t *testing.T) {
			t.Parallel()
			db, releases, now := retirementFixture(t)
			t.Cleanup(releases.Close)
			seedExpiringFirmwarePlay(t, db, now)
			repo := retirement.NewRetirement(db)
			var before application.LaunchRetirement
			err := repo.WithRetirement(t.Context(), func(scope application.RetirementScope) error {
				var err error
				before, err = scope.Read.Launch(t.Context(), now, 200)
				return err
			})
			if err != nil || !before.Found {
				t.Fatalf("read retiring launch: %+v %v", before, err)
			}
			if renew {
				_, err = updateFirmwareLaunch(t, db, recordstore.Update{
					Set:   `idle_expires_at_ms=?,updated_at_ms=?,version=version+1`,
					Scope: recordstore.Scope{Where: `id='firmware-launch'`}, Values: []any{now + 1000, now},
				})
			} else {
				_, err = db.ExecContext(t.Context(), `INSERT INTO launch_external_files(
launch_session_id,virtual_path,logical_name,blob_id,created_at_ms,kind)
SELECT launch_session_id,'/bios/later.bin','later.bin',blob_id,created_at_ms,kind
FROM launch_external_files WHERE launch_session_id='firmware-launch' AND logical_name='gba_bios.bin'`)
			}
			if err != nil {
				t.Fatal(err)
			}
			err = repo.WithRetirement(t.Context(), func(scope application.RetirementScope) error {
				if err := scope.Launch.FenceLaunch(t.Context(), before); err != nil {
					return err
				}
				if err := scope.Launch.TerminateLaunch(t.Context(), application.LaunchRetirementEnd{
					Before: before, Expire: true, State: "EXPIRED", PlayState: "ABANDONED", NowMS: now,
				}); err != nil {
					return err
				}
				if err := scope.Launch.ReleaseLaunchFiles(t.Context(), before); err != nil {
					return err
				}
				return scope.Launch.CompleteLaunch(t.Context(), application.RetirementCompletion{ID: before.ID, DueMS: now, NowMS: now})
			})
			if !errors.Is(err, application.ErrRetirementSnapshotChanged) {
				t.Fatalf("stale retirement committed: %v", err)
			}
			if !renew {
				if _, err := db.ExecContext(t.Context(), `DELETE FROM launch_external_files
WHERE launch_session_id='firmware-launch' AND virtual_path='/bios/later.bin'`); err != nil {
					t.Fatal(err)
				}
			}
			assertFirmwareRetirementUnchanged(t, db)
		})
	}
}

func TestRetirementRejectsZeroCompletionRows(t *testing.T) {
	t.Parallel()
	for _, bios := range []bool{true, false} {
		t.Run(map[bool]string{true: "BIOS", false: "launch"}[bios], func(t *testing.T) {
			t.Parallel()
			db, releases, now := retirementFixture(t)
			t.Cleanup(releases.Close)
			prefix, fragment := "UPDATE launch_payload_retirements SET released_at_ms=", "WHERE"
			if bios {
				seedRetiringInstallation(t, db, "zero-installation", 0, now)
				prefix, fragment = "UPDATE bios_installations SET", "blob_id=NULL"
			} else {
				seedExpiringFirmwarePlay(t, db, now)
			}
			var hits atomic.Int64
			service := faultRetirementService(t, db, now, prefix, fragment, nil, &hits)
			err := service.ReconcileGC(t.Context())
			if !errors.Is(err, application.ErrRetirementSnapshotChanged) || hits.Load() != 1 {
				t.Fatalf("zero row count accepted: hits=%d err=%v", hits.Load(), err)
			}
			assertBIOSReferenceCounts(t, db, 1, 1)
			if !bios {
				assertFirmwareRetirementUnchanged(t, db)
			}
		})
	}
}
