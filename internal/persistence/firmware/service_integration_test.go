//go:build integration

package firmware

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"hash/crc32"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	firmwareservice "retrom/internal/service/firmware"

	uploadpersistence "retrom/internal/persistence/uploads"

	dependencypersistence "retrom/internal/persistence/dependencies"
	dependencyservice "retrom/internal/service/dependencies"

	dbapi "retrom/internal/database"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/sessionstore"

	"retrom/internal/cleanup"
	"retrom/internal/composition/cleanupjobs"
	"retrom/internal/dependencies"
	"retrom/internal/filestore"
	"retrom/internal/legacychecksum"
	"retrom/internal/service/uploads"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

func TestStaticBIOSHashMismatchIsInstalledAsWarning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencyservice.New(dependencySet,
		dependencypersistence.New(database.SQL)).Bootstrap(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := filestore.Open(dataDir)
	testassert.False(t, err != nil, err)
	contents := []byte("retrom-invalid-bios\n")
	uploadService := uploads.New(uploadpersistence.New(database.SQL), blobs, dataDir, time.Now)
	upload, err := uploadService.Create(
		ctx,
		uploads.CreateRequest{
			SourceType: "FILES",
			Files: []uploads.FileDeclaration{
				{ClientFileID: "bios", RelativePath: "gba_bios.bin", SizeBytes: int64(len(contents))},
			},
		},
	)
	testassert.False(t, err != nil, err)
	digest := sha256.Sum256(contents)
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0,
		fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)),
		"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := uploadService.Get(ctx, upload.ID)
	jobID, _, err := uploadService.Complete(ctx, upload.ID, snapshot.Version)
	testassert.False(t, err != nil, err)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		_ = dbapi.QueryRowContext(ctx, database.SQL, `
SELECT state
FROM jobs
WHERE id=?
`, jobID).Scan(&state)
		if state == "SUCCEEDED" {
			break
		}
		testassert.Falsef(t, time.Now().After(deadline), "finalize state = %s", state)
		time.Sleep(10 * time.Millisecond)
	}
	var requirementID string
	var version int64
	if err := dbapi.QueryRowContext(ctx, database.SQL, `
SELECT id,
version
FROM bios_requirements
WHERE core_id='mgba'
AND logical_name='gba_bios.bin'
AND enabled=1
`).Scan(&requirementID, &version); err != nil {
		t.Fatal(err)
	}
	runtimeIdentity, err := testsupport.LookupRuntimeTarget(ctx, database.SQL, "mgba")
	testassert.False(t, err != nil, err)
	var md5Value, sha1Value, sha256Value string
	if err := dbapi.QueryRowContext(ctx, database.SQL, `
SELECT json_extract(b.value, '$.md5'),
json_extract(b.value, '$.sha1'),
json_extract(b.value, '$.sha256')
FROM upload_files f
JOIN json_each(json_array(f.final_file_record)) b ON b.value IS NOT NULL
WHERE f.id=?
`, upload.Files[0].ID).Scan(&md5Value, &sha1Value, &sha256Value); err != nil {
		t.Fatal(err)
	}
	releases, err := cleanupjobs.New(t.Context(), database.SQL, blobs, time.Now)
	testassert.False(t, err != nil, err)
	service := firmwareservice.New(firmwareservice.Dependencies{Repository: New(database.SQL), Files: blobs, Cleanup: releases}, time.Now)
	result, err := service.Install(ctx, requirementID, version,
		firmwareservice.InstallRequest{UploadFileID: upload.Files[0].ID})
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return result.Status != "HASH_WARNING" },
		func() bool { return !result.Active }), "installation = %#v", result)
	var oldFileRecord string
	if err := dbapi.QueryRowContext(ctx, database.SQL, `SELECT file_record FROM bios_installations WHERE id=?`,
		result.InstallationID).Scan(&oldFileRecord); err != nil {
		t.Fatal(err)
	}
	lifecycle := seedFirmwareReplacementLifecycle(
		t, ctx, database.SQL, blobs, runtimeIdentity, result.InstallationID, oldFileRecord,
	)
	replacementFileID := completeFirmwareUpload(
		t, ctx, database.SQL, uploadService, "gba_bios.bin", []byte("retrom-replacement-bios\n"),
	)
	replaced, err := service.Install(
		ctx, requirementID, version, firmwareservice.InstallRequest{UploadFileID: replacementFileID},
	)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, replaced.InstallationID == result.InstallationID,
		"replacement reused installation %s", replaced.InstallationID)
	assertFirmwareReplacementLifecycle(t, ctx, database.SQL, lifecycle)
	assertDeferredBIOSRelease(t, ctx, database.SQL, releases, lifecycle, oldFileRecord, result.InstallationID)
}

type firmwareReplacementLifecycle struct {
	variantID          string
	launchID           string
	saveID             string
	payloadFileRecords []string
}

func seedFirmwareReplacementLifecycle(
	t *testing.T,
	ctx context.Context,
	database dbapi.DB,
	blobs *filestore.Store,
	runtimeIdentity testsupport.RuntimeTargetIdentity, installationID, biosFileRecord string,
) firmwareReplacementLifecycle {
	t.Helper()
	contentFileRecord := ensureFirmwareBlob(t, ctx, database, blobs, []byte("firmware-game-content"))
	stateFileRecord := ensureFirmwareBlob(t, ctx, database, blobs, []byte("firmware-save-state"))
	screenshotFileRecord := ensureFirmwareBlob(t, ctx, database, blobs, []byte("firmware-save-screenshot"))
	now := time.Now().UnixMilli()
	snapshot := fmt.Sprintf(
		`{"schemaVersion":1,"kind":"STATIC","bios":[{"installationId":%q,"fileRecord":%q}]}`,
		installationID, biosFileRecord,
	)
	statePayload := []byte("firmware-save-state")
	stateDigest := fmt.Sprintf("%x", sha256.Sum256(statePayload))
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		query      string
		args       []any
		references string
	}{
		{`INSERT INTO platform_instances(id,platform_id,default_core_id,name,slug,enabled,created_at_ms,
updated_at_ms)
VALUES('firmware-platform','gba','mgba','Firmware GBA','firmware-gba',1,?,?)`, []any{now, now}, ""},
		{`INSERT INTO games(
id,platform_instance_id,title,title_initial,description,developer,publisher,genre,
metadata_source_kind,content_kind,content_source_kind,
source_manifest_json,source_manifest_digest,status,search_text,version,created_at_ms,updated_at_ms)
VALUES('firmware-game','firmware-platform','Firmware','F','','','','','ADMIN_EDIT','SINGLE_FILE',
'ADMIN_REPLACE','{}',?,'PUBLISHED','firmware',1,?,?)`, []any{strings.Repeat("1", 64), now, now}, ""},
		{`INSERT INTO game_files(game_id,role,logical_name,file_record,sort_order)
VALUES('firmware-game','CONTENT','firmware.gba',?,0)`, []any{contentFileRecord}, "game_files"},
		{
			`INSERT INTO game_variants(
id,game_id,core_id,provider_id,target_id,dat_version_id,emulator_game_id,status,
compatibility_code,dependency_snapshot_json,version,created_at_ms,updated_at_ms)
VALUES('firmware-variant','firmware-game','mgba',?,?,NULL,800001,'READY','READY',?,1,?,?)`,
			[]any{runtimeIdentity.ProviderID, runtimeIdentity.TargetID, snapshot, now, now},
			"",
		},
		{`INSERT INTO variant_files(game_variant_id,role,logical_name,file_record,sort_order)
VALUES('firmware-variant','BIOS_BUNDLE','gba_bios.bin',?,0)`, []any{biosFileRecord}, "variant_files"},
		{`INSERT INTO profiles(id,display_name,created_at_ms) VALUES('firmware-profile','Firmware',?)`, []any{now}, ""},
		{`INSERT INTO launch_sessions(id,profile_id,game_id,core_id,provider_id,target_id,bundle_sha256,
content_kind,dependency_snapshot_json,compatibility_code,return_to,credential_sha256,state,
bootstrap_expires_at_ms,activated_at_ms,
hard_expires_at_ms,created_at_ms,updated_at_ms)
VALUES('firmware-launch','firmware-profile','firmware-game','mgba',?,?,?,
'SINGLE_FILE',?,'READY','/',?,'ACTIVE',?,?,?,?,?)`, []any{
			runtimeIdentity.ProviderID, runtimeIdentity.TargetID, runtimeIdentity.BundleSHA256, snapshot, make([]byte, 32),
			now + 60_000, now, now + 120_000, now, now,
		}, ""},
		{`INSERT INTO launch_content_files(launch_session_id,logical_name,file_record,format_version,created_at_ms)
VALUES('firmware-launch','firmware.gba',?,'SOURCE_V1',?)`, []any{contentFileRecord, now}, ""},
		{`INSERT INTO launch_external_files(launch_session_id,virtual_path,logical_name,file_record,created_at_ms,kind)
VALUES('firmware-launch','/bios/gba_bios.bin','gba_bios.bin',?,?,'BIOS_BUNDLE')`, []any{biosFileRecord, now}, ""},
		{
			`INSERT INTO save_states(id,profile_id,game_id,checkpoint_format,payload_file_record,payload_sha256,
payload_size_bytes,screenshot_file_record,name,active_duration_ms,created_at_ms,updated_at_ms,
source_launch_session_id)
VALUES('firmware-save','firmware-profile','firmware-game','test-checkpoint-v1',?,?,?,?,'Firmware save',1,
?,?,'firmware-launch')`,
			[]any{
				stateFileRecord, stateDigest, len(statePayload), screenshotFileRecord, now, now,
			},
			"save_states",
		},
	}
	for _, statement := range statements {
		execute := func(ctx context.Context, query string, args ...any) (sql.Result, error) {
			return testsupport.ExecuteSeed(ctx, transaction, statement.references, query, args...)
		}
		if strings.HasPrefix(statement.query, "INSERT INTO launch_sessions(") {
			execute = func(ctx context.Context, query string, args ...any) (sql.Result, error) {
				return sessionstore.CreateLaunch(ctx, transaction, query, args...)
			}
		}
		if _, err := execute(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	return firmwareReplacementLifecycle{
		variantID: "firmware-variant", launchID: "firmware-launch", saveID: "firmware-save",
		payloadFileRecords: []string{stateFileRecord, screenshotFileRecord},
	}
}

func ensureFirmwareBlob(
	t *testing.T,
	ctx context.Context,
	database dbapi.DB,
	blobs *filestore.Store,
	contents []byte,
) string {
	t.Helper()
	metadata, err := blobs.Put(bytes.NewReader(contents))
	if err != nil {
		t.Fatal(err)
	}
	fileRecord, err := filestore.FileRecord(metadata, "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	return fileRecord
}

func assertFirmwareReplacementLifecycle(
	t *testing.T,
	ctx context.Context,
	database dbapi.DB,
	lifecycle firmwareReplacementLifecycle,
) {
	t.Helper()
	var variantStatus, compatibilityCode string
	if err := dbapi.QueryRowContext(ctx, database,
		`SELECT status,compatibility_code FROM game_variants WHERE id=?`, lifecycle.variantID,
	).Scan(&variantStatus, &compatibilityCode); err != nil {
		t.Fatal(err)
	}
	if variantStatus != "READY" || compatibilityCode != "READY" {
		t.Fatalf("BIOS replacement must preserve current validation until next launch: %s/%s",
			variantStatus, compatibilityCode)
	}
	var variantFiles, saves, launchFiles int
	var launchState string
	if err := dbapi.QueryRowContext(ctx, database, `
SELECT
 (SELECT count(*) FROM variant_files WHERE game_variant_id=?),
 (SELECT count(*) FROM save_states WHERE id=?),
 (SELECT state FROM launch_sessions WHERE id=?),
 (SELECT count(*) FROM launch_content_files WHERE launch_session_id=?)
`, lifecycle.variantID, lifecycle.saveID, lifecycle.launchID, lifecycle.launchID).
		Scan(&variantFiles, &saves, &launchState, &launchFiles); err != nil {
		t.Fatal(err)
	}
	if variantFiles != 1 || saves != 1 || launchState != "REVOKED" || launchFiles != 0 {
		t.Fatalf(
			"BIOS replacement lifecycle = variant files %d, saves %d, launch %s, launch files %d",
			variantFiles, saves, launchState, launchFiles,
		)
	}
	for _, fileRecord := range lifecycle.payloadFileRecords {
		var candidates int
		if err := dbapi.QueryRowContext(
			ctx, database,
			`SELECT count(*) FROM job_input_snapshots WHERE json_extract(?,'$.path') LIKE json_extract(input_json,
'$.inputs.relativePath') || '/%'`, fileRecord,
		).Scan(&candidates); err != nil || candidates != 0 {
			t.Fatalf("BIOS replacement payload %s candidates = %d, error=%v", fileRecord, candidates, err)
		}
	}
}

func completeFirmwareUpload(
	t *testing.T,
	ctx context.Context,
	database dbapi.DB,
	service *uploads.Service,
	name string,
	contents []byte,
) string {
	t.Helper()
	upload, err := service.Create(ctx, uploads.CreateRequest{
		SourceType: "FILES",
		Files:      []uploads.FileDeclaration{{ClientFileID: "bios", RelativePath: name, SizeBytes: int64(len(contents))}},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	if err := service.PutPart(
		ctx, upload.ID, upload.Files[0].ID, 0,
		fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)),
		"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents),
	); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Get(ctx, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := service.Complete(ctx, upload.ID, snapshot.Version)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		if err := dbapi.QueryRowContext(ctx, database, `SELECT state FROM jobs WHERE id=?`, jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			return upload.Files[0].ID
		}
		if state == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("firmware upload finalize state = %s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDATMachineBIOSScansUploadAndAcceptsContentMatchedFilenameAlias(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencyservice.New(dependencySet,
		dependencypersistence.New(database.SQL)).Bootstrap(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	runtimeIdentity, err := testsupport.LookupRuntimeTarget(ctx, database.SQL, "mame2003_plus")
	testassert.False(t, err != nil, err)
	contents := []byte("deterministic ST-V BIOS fixture")
	_, sha1Value := legacychecksum.Sum(contents)
	crc32Value := fmt.Sprintf("%08x", crc32.ChecksumIEEE(contents))
	now := time.Now().UnixMilli()
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_versions(id,core_id,provider_id,target_id,builtin_relative_path,sha256,parser_version,
parse_status,is_active,machine_count,rom_entry_count,disk_entry_count,bios_set_count,
default_bios_set_count,explicit_bios_machine_count,base_dependency_target_count,unresolved_relation_count,
version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
VALUES('dat-test','mame2003_plus',?,?,'test.dat',?,'test-parser','READY',1,1,1,0,1,1,1,0,0,1,?,?,?,?)
`, runtimeIdentity.ProviderID, runtimeIdentity.TargetID, strings.Repeat("a", 64), now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_machines(dat_version_id,machine_name,description,year,manufacturer,is_explicit_bios,
classification)
VALUES('dat-test','stvbios','ST-V BIOS','','SEGA',1,'EXPLICIT_BIOS')
`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_bios_sets(dat_version_id,machine_name,bios_name,description,is_default)
VALUES('dat-test','stvbios','japan','Japan',1),
('dat-test','stvbios','usa','USA',0)
`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_rom_entries(dat_version_id,machine_name,ordinal,name,size_bytes,crc32,sha1,status,bios_name)
VALUES('dat-test','stvbios',0,'epr19730.ic8',?,?,?,'GOOD','japan')
`, len(contents), crc32Value, sha1Value); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_rom_entries(dat_version_id,machine_name,ordinal,name,size_bytes,crc32,sha1,status,bios_name)
VALUES('dat-test','stvbios',1,'non-default.bin',4,'00000000',?,'GOOD','usa')
`, strings.Repeat("0", 40)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO bios_requirements(id,core_id,provider_id,target_id,source_kind,dat_machine_name,logical_name,
requirement_mode,condition_code,catalog_digest,source_url,source_version,enabled,version,created_at_ms,
updated_at_ms)
VALUES('requirement-test','mame2003_plus',?,?,'DAT_MACHINE','stvbios','stvbios.zip','REQUIRED',
'ARCADE_DAT_DEPENDENCY',?,'retrom:test','dat-test',1,1,?,?)
`, runtimeIdentity.ProviderID, runtimeIdentity.TargetID, strings.Repeat("b", 64), now, now); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create("epr-19730.ic8")
	testassert.False(t, err != nil, err)
	if _, err := entry.Write(contents); err != nil {
		t.Fatal(err)
	}
	extraEntry, err := writer.Create("notes/extra.bin")
	testassert.False(t, err != nil, err)
	if _, err := extraEntry.Write([]byte("extra")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	blobs, err := filestore.Open(dataDir)
	testassert.False(t, err != nil, err)
	uploadService := uploads.New(uploadpersistence.New(database.SQL), blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{
		SourceType: "FILES",
		Files: []uploads.FileDeclaration{{
			ClientFileID: "bios", RelativePath: "stvbios.zip",
			SizeBytes: int64(archive.Len()),
		}},
	})
	testassert.False(t, err != nil, err)
	digest := sha256.Sum256(archive.Bytes())
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0,
		fmt.Sprintf("bytes 0-%d/%d", archive.Len()-1, archive.Len()),
		"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":",
		bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := uploadService.Get(ctx, upload.ID)
	jobID, _, err := uploadService.Complete(ctx, upload.ID, snapshot.Version)
	testassert.False(t, err != nil, err)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		_ = dbapi.QueryRowContext(ctx, database.SQL, `SELECT state FROM jobs WHERE id=?`, jobID).Scan(&state)
		if state == "SUCCEEDED" {
			break
		}
		testassert.Falsef(t, time.Now().After(deadline), "finalize state = %s", state)
		time.Sleep(10 * time.Millisecond)
	}
	result, err := firmwareservice.New(firmwareservice.Dependencies{Repository: New(database.SQL), Files: blobs}, time.Now).Install(
		ctx, "requirement-test", 1, firmwareservice.InstallRequest{UploadFileID: upload.Files[0].ID},
	)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return result.Status != "MATCHED" },
		func() bool { return !result.Active }), "installation = %#v", result)
	warnings, ok := result.ValidationDetails["warnings"].([]string)
	testassert.Falsef(t, testassert.Any(func() bool { return !ok },
		func() bool { return len(warnings) != 1 }, func() bool {
			return !strings.Contains(warnings[0],
				"epr-19730.ic8")
		}), "alias warnings = %#v", result.ValidationDetails["warnings"])
	inspection, err := firmwareservice.New(firmwareservice.Dependencies{Repository: New(database.SQL), Files: constructorFiles(t)}, time.Now).InspectArchive(ctx, "requirement-test")
	testassert.False(t, err != nil, err)
	testassert.Falsef(t,
		testassert.Any(func() bool { return inspection.LogicalName != "stvbios.zip" },
			func() bool { return inspection.InstallationStatus != "MATCHED" },
			func() bool { return len(inspection.Entries) != 2 }), "inspection = %#v", inspection)
	if comparison := inspection.Entries[0]; comparison.Status != "ALIASED" ||
		comparison.Expected == nil || comparison.Expected.Name != "epr19730.ic8" ||
		comparison.Actual == nil || comparison.Actual.Name != "epr-19730.ic8" ||
		comparison.Expected.SizeBytes != int64(len(contents)) || comparison.Expected.CRC32 != crc32Value {
		t.Fatalf("required entry comparison = %#v", comparison)
	}
	if comparison := inspection.Entries[1]; comparison.Status != "EXTRA" ||
		comparison.Expected != nil || comparison.Actual == nil || comparison.Actual.Name != "notes/extra.bin" {
		t.Fatalf("extra entry comparison = %#v", comparison)
	}
	var indexed int64
	if err := dbapi.QueryRowContext(ctx, database.SQL, `SELECT count(*) FROM archive_entries`).Scan(&indexed); err != nil || indexed != 2 {
		t.Fatalf("archive entries = %d, error=%v", indexed, err)
	}
}

func updateFirmwareLaunch(t *testing.T, db dbapi.DB, change recordstore.Update) (sql.Result, error) {
	t.Helper()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer dbapi.Rollback(tx)
	result, err := sessionstore.ChangeLaunch(t.Context(), tx, change)
	if err != nil {
		return nil, err
	}
	return result, tx.Commit()
}
