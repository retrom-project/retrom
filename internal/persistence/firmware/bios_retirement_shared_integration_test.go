//go:build integration

package firmware

import (
	"bytes"
	"database/sql"
	"fmt"
	"testing"

	"retrom/internal/filestore"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"

	firmwareservice "retrom/internal/service/firmware"
	"retrom/internal/testassert"
)

func TestBIOSRetirementPreservesIndependentReinstallation(t *testing.T) {
	t.Parallel()
	database, releases, now := retirementFixture(t)
	seedRetiringInstallation(t, database, "018fbe68-0000-7000-8000-000000000031", 0, now)
	seedRetiringInstallation(t, database, "018fbe68-0000-7000-8000-000000000032", 1, now)
	testassert.False(t, releases.ReconcileDeletion(t.Context()) != nil, "reconcile independent BIOS")
	assertBIOSReferenceCounts(t, database, 0, 1)
	var active int
	err := dbapi.QueryRowContext(t.Context(), database, `SELECT count(*) FROM bios_installations WHERE id='018fbe68-0000-7000-8000-000000000032' AND is_active=1
AND file_record IS NOT NULL`).Scan(&active)
	testassert.False(t, err != nil, err)
	testassert.True(t, active == 1, "new BIOS released")
	tx, err := database.BeginTx(t.Context(), nil)
	testassert.False(t, err != nil, err)
	var requirement string
	err = dbapi.QueryRowContext(t.Context(), tx, `SELECT requirement_id FROM bios_installations WHERE id='018fbe68-0000-7000-8000-000000000032'`).Scan(&requirement)
	testassert.False(t, err != nil, err)
	testassert.False(t, firmwareservice.SupersedeInScope(t.Context(), BindSupersession(tx),
		requirement, now) != nil, "supersede")
	testassert.False(t, tx.Rollback() != nil, "rollback failed replacement")
	err = dbapi.QueryRowContext(t.Context(), database, `SELECT is_active FROM bios_installations WHERE id='018fbe68-0000-7000-8000-000000000032'`).Scan(&active)
	testassert.False(t, err != nil, err)
	testassert.True(t, active == 1, "rollback lost active installation")
}

func TestBIOSRetirementDrainsMultipleBatchesWithoutTouchingLiveLaunch(t *testing.T) {
	t.Parallel()
	database, releases, now := retirementFixture(t)
	seedRetiringInstallation(t, database, "018fbe68-0000-7000-8000-000000000031", 0, now)
	for i := 0; i < 205; i++ {
		_, err := recordstore.InsertRows(t.Context(), database, "variant_files", `INSERT INTO variant_files(game_variant_id,role,logical_name,file_record,sort_order)
SELECT game_variant_id,role,?,file_record,? FROM variant_files WHERE logical_name='gba_bios.bin'`, fmt.Sprintf("bios-%03d.bin", i), i+1)
		testassert.False(t, err != nil, err)
	}
	testassert.False(t, releases.ReconcileDeletion(t.Context()) != nil, "drain retirements")
	testassert.False(t, releases.ReconcileDeletion(t.Context()) != nil, "idempotent retirement")
	assertBIOSReferenceCounts(t, database, 0, 1)
	var pending int
	err := dbapi.QueryRowContext(t.Context(), database, `SELECT count(*) FROM bios_installations WHERE is_active=0 AND file_record IS NOT NULL`).Scan(&pending)
	testassert.False(t, err != nil, err)
	testassert.True(t, pending == 0, "pending retirement stranded after batch boundary")
}

func seedRetiringInstallation(t *testing.T, database dbapi.DB, id string, active int, now int64) {
	t.Helper()
	var fileID string
	err := dbapi.QueryRowContext(t.Context(), database, `SELECT file_record FROM variant_files WHERE game_variant_id='firmware-variant' AND logical_name='gba_bios.bin'`).Scan(&fileID)
	testassert.False(t, err != nil, err)
	if active == 1 {
		files, err := filestore.Open(t.TempDir())
		testassert.False(t, err != nil, err)
		metadata, err := files.Put(bytes.NewReader([]byte("retirement BIOS")))
		testassert.False(t, err != nil, err)
		fileID, err = filestore.FileRecord(metadata, "application/octet-stream")
		testassert.False(t, err != nil, err)
	}
	_, err = recordstore.InsertRows(t.Context(), database, "bios_installations", `INSERT INTO bios_installations(
id,requirement_id,file_record,original_filename,size_bytes,md5,sha1,sha256,validated_requirement_version,
status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
SELECT ?,requirement.id,blob.value,'gba_bios.bin',(((blob.value)::jsonb #>> '{size_bytes}'))::bigint,
((blob.value)::jsonb #>> '{md5}'),((blob.value)::jsonb #>> '{sha1}'),((blob.value)::jsonb #>> '{sha256}'),
requirement.version,
'HASH_WARNING','{}',?,1,?,? FROM bios_requirements requirement
JOIN LATERAL (SELECT ? AS value) blob ON blob.value IS NOT NULL WHERE requirement.core_id='mgba' AND
requirement.logical_name='gba_bios.bin' AND requirement.enabled=1`, id, active, now, now, fileID)
	testassert.False(t, err != nil, err)
}

func assertBIOSReferenceCounts(t *testing.T, database dbapi.DB, variants, launches int) {
	t.Helper()
	var actualVariants, actualLaunches int
	err := dbapi.QueryRowContext(t.Context(), database, `SELECT (SELECT count(*) FROM variant_files WHERE game_variant_id='firmware-variant'),
(SELECT count(*) FROM launch_external_files WHERE launch_session_id='firmware-launch')`).Scan(&actualVariants, &actualLaunches)
	testassert.False(t, err != nil, err)
	testassert.True(t, actualVariants == variants && actualLaunches == launches,
		"shared or active BIOS references changed unexpectedly")
}

func TestFinishedLaunchRetirementDrainsLargeFileSets(t *testing.T) {
	t.Parallel()
	database, releases, now := retirementFixture(t)
	for i := 0; i < 205; i++ {
		_, err := database.ExecContext(t.Context(), `INSERT INTO launch_external_files(launch_session_id,virtual_path,logical_name,file_record,created_at_ms,kind)
SELECT launch_session_id,?, ?,file_record,created_at_ms,kind FROM launch_external_files
WHERE launch_session_id='firmware-launch' AND logical_name='gba_bios.bin'`, fmt.Sprintf("/bios/%03d.bin", i), fmt.Sprintf("%03d.bin", i))
		testassert.False(t, err != nil, err)
	}
	_, err := updateFirmwareLaunch(t, database, recordstore.Update{Set: `state='FINISHED',finished_at_ms=?,updated_at_ms=?,version=version+1`, Scope: recordstore.Scope{Where: `id='firmware-launch'`}, Values: []any{now, now}})
	testassert.False(t, err != nil, err)
	testassert.False(t, releases.ReconcileDeletion(t.Context()) != nil, "drain large launch")
	assertBIOSReferenceCounts(t, database, 1, 0)
	var released sql.NullInt64
	err = dbapi.QueryRowContext(t.Context(), database, `SELECT released_at_ms FROM launch_payload_retirements WHERE launch_session_id='firmware-launch'`).Scan(&released)
	testassert.False(t, err != nil, err)
	testassert.True(t, released.Valid, "large launch left pending after complete drain")
}
