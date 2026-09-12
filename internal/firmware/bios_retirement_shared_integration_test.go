//go:build integration

package firmware

import (
	"database/sql"
	"fmt"
	"testing"

	"retrom/internal/recordstore"

	"retrom/internal/payloadrelease"
	"retrom/internal/testassert"
)

func TestBIOSRetirementPreservesReinstalledSharedBytes(t *testing.T) {
	t.Parallel()
	database, releases, now := retirementFixture(t)
	seedRetiringInstallation(t, database, "old-install", 0, now)
	seedRetiringInstallation(t, database, "new-install", 1, now)
	testassert.False(t, releases.ReconcileGC(t.Context()) != nil, "reconcile shared BIOS")
	assertBIOSReferenceCounts(t, database, 1, 1)
	var active int
	err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM bios_installations WHERE id='new-install' AND is_active=1 AND blob_id IS NOT NULL`).Scan(&active)
	testassert.False(t, err != nil, err)
	testassert.True(t, active == 1, "shared active BIOS released")
	tx, err := database.BeginTx(t.Context(), nil)
	testassert.False(t, err != nil, err)
	var requirement string
	err = tx.QueryRowContext(t.Context(), `SELECT requirement_id FROM bios_installations WHERE id='new-install'`).Scan(&requirement)
	testassert.False(t, err != nil, err)
	testassert.False(t, payloadrelease.SupersedeBIOS(t.Context(), tx, requirement, now) != nil, "supersede")
	testassert.False(t, tx.Rollback() != nil, "rollback failed replacement")
	err = database.QueryRowContext(t.Context(), `SELECT is_active FROM bios_installations WHERE id='new-install'`).Scan(&active)
	testassert.False(t, err != nil, err)
	testassert.True(t, active == 1, "rollback lost active installation")
}

func TestBIOSRetirementDrainsMultipleBatchesWithoutTouchingLiveLaunch(t *testing.T) {
	t.Parallel()
	database, releases, now := retirementFixture(t)
	seedRetiringInstallation(t, database, "old-install", 0, now)
	for i := 0; i < 205; i++ {
		_, err := database.ExecContext(t.Context(), `INSERT INTO variant_files(game_variant_id,role,logical_name,blob_id,sort_order)
SELECT game_variant_id,role,?,blob_id,? FROM variant_files WHERE logical_name='gba_bios.bin'`, fmt.Sprintf("bios-%03d.bin", i), i+1)
		testassert.False(t, err != nil, err)
	}
	testassert.False(t, releases.ReconcileGC(t.Context()) != nil, "drain retirements")
	testassert.False(t, releases.ReconcileGC(t.Context()) != nil, "idempotent retirement")
	assertBIOSReferenceCounts(t, database, 0, 1)
	var pending int
	err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM bios_installations WHERE is_active=0 AND blob_id IS NOT NULL`).Scan(&pending)
	testassert.False(t, err != nil, err)
	testassert.True(t, pending == 0, "pending retirement stranded after batch boundary")
}

func seedRetiringInstallation(t *testing.T, database *sql.DB, id string, active int, now int64) {
	t.Helper()
	_, err := database.ExecContext(t.Context(), `INSERT INTO bios_installations(
id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,validated_requirement_version,
status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
SELECT ?,requirement.id,blob.id,'gba_bios.bin',blob.size_bytes,blob.md5,blob.sha1,blob.sha256,requirement.version,
'HASH_WARNING','{}',?,1,?,? FROM bios_requirements requirement
JOIN variant_files file ON file.game_variant_id='firmware-variant' AND file.logical_name='gba_bios.bin'
JOIN blobs blob ON blob.id=file.blob_id WHERE requirement.core_id='mgba' AND requirement.logical_name='gba_bios.bin' AND requirement.enabled=1`, id, active, now, now)
	testassert.False(t, err != nil, err)
}

func assertBIOSReferenceCounts(t *testing.T, database *sql.DB, variants, launches int) {
	t.Helper()
	var actualVariants, actualLaunches int
	err := database.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM variant_files WHERE game_variant_id='firmware-variant'),
(SELECT count(*) FROM launch_external_files WHERE launch_session_id='firmware-launch')`).Scan(&actualVariants, &actualLaunches)
	testassert.False(t, err != nil, err)
	testassert.True(t, actualVariants == variants && actualLaunches == launches, "shared or active BIOS references changed unexpectedly")
}

func TestFinishedLaunchRetirementDrainsLargeFileSets(t *testing.T) {
	t.Parallel()
	database, releases, now := retirementFixture(t)
	for i := 0; i < 205; i++ {
		_, err := database.ExecContext(t.Context(), `INSERT INTO launch_external_files(launch_session_id,virtual_path,logical_name,blob_id,created_at_ms,kind)
SELECT launch_session_id,?, ?,blob_id,created_at_ms,kind FROM launch_external_files
WHERE launch_session_id='firmware-launch' AND logical_name='gba_bios.bin'`, fmt.Sprintf("/bios/%03d.bin", i), fmt.Sprintf("%03d.bin", i))
		testassert.False(t, err != nil, err)
	}
	_, err := updateFirmwareLaunch(t, database, recordstore.Update{Set: `state='FINISHED',finished_at_ms=?,updated_at_ms=?,version=version+1`, Scope: recordstore.Scope{Where: `id='firmware-launch'`}, Values: []any{now, now}})
	testassert.False(t, err != nil, err)
	testassert.False(t, releases.ReconcileGC(t.Context()) != nil, "drain large launch")
	assertBIOSReferenceCounts(t, database, 1, 0)
	var released sql.NullInt64
	err = database.QueryRowContext(t.Context(), `SELECT released_at_ms FROM launch_payload_retirements WHERE launch_session_id='firmware-launch'`).Scan(&released)
	testassert.False(t, err != nil, err)
	testassert.True(t, released.Valid, "large launch left pending after complete drain")
}
