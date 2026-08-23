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

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/legacychecksum"
	"retrom/internal/payloadrelease"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestStaticBIOSHashMismatchIsInstalledAsWarning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	contents := []byte("retrom-invalid-bios\n")
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
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
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)), "sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := uploadService.Get(ctx, upload.ID)
	jobID, _, err := uploadService.Complete(ctx, upload.ID, snapshot.Version)
	testassert.False(t, err != nil, err)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		_ = database.SQL.QueryRowContext(ctx, `
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
	var requirementID, artifactID string
	var version int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id,
version,
core_artifact_id
FROM bios_requirements
WHERE core_id='mgba'
AND logical_name='gba_bios.bin'
AND enabled=1
`).Scan(&requirementID, &version, &artifactID); err != nil {
		t.Fatal(err)
	}
	var md5Value, sha1Value, sha256Value string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT b.md5,
b.sha1,
b.sha256
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
WHERE f.id=?
`, upload.Files[0].ID).Scan(&md5Value, &sha1Value, &sha256Value); err != nil {
		t.Fatal(err)
	}
	releases, err := payloadrelease.New(database.SQL, blobs, time.Now, 7*24*time.Hour)
	testassert.False(t, err != nil, err)
	releases.Start()
	t.Cleanup(releases.Close)
	service := New(database.SQL, time.Now).WithBlobStore(blobs).WithPayloadRelease(releases)
	result, err := service.Install(ctx, requirementID, version, InstallRequest{UploadFileID: upload.Files[0].ID})
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return result.Status != "HASH_WARNING" }, func() bool { return !result.Active }), "installation = %#v", result)
	var oldBlobID string
	if err := database.SQL.QueryRowContext(ctx, `SELECT blob_id FROM bios_installations WHERE id=?`,
		result.InstallationID).Scan(&oldBlobID); err != nil {
		t.Fatal(err)
	}
	lifecycle := seedFirmwareReplacementLifecycle(
		t, ctx, database.SQL, blobs, artifactID, result.InstallationID, oldBlobID,
	)
	replacementFileID := completeFirmwareUpload(
		t, ctx, database.SQL, uploadService, "gba_bios.bin", []byte("retrom-replacement-bios\n"),
	)
	replaced, err := service.Install(
		ctx, requirementID, version, InstallRequest{UploadFileID: replacementFileID},
	)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, replaced.InstallationID == result.InstallationID, "replacement reused installation %s", replaced.InstallationID)
	deadline = time.Now().Add(3 * time.Second)
	for {
		var releasedAt sql.NullInt64
		var oldBlob sql.NullString
		var candidates int
		if err := database.SQL.QueryRowContext(ctx, `
SELECT installation.blob_id,installation.payload_released_at_ms,
 (SELECT count(*) FROM blob_gc_candidates WHERE blob_id=?)
FROM bios_installations installation WHERE installation.id=?
`, oldBlobID, result.InstallationID).Scan(&oldBlob, &releasedAt, &candidates); err != nil {
			t.Fatal(err)
		}
		if !oldBlob.Valid && releasedAt.Valid && candidates == 1 {
			break
		}
		testassert.Falsef(t, time.Now().After(deadline),
			"retired BIOS = blob %v, released %v, candidates %d", oldBlob, releasedAt, candidates)
		time.Sleep(10 * time.Millisecond)
	}
	assertFirmwareReplacementLifecycle(t, ctx, database.SQL, lifecycle)
}

type firmwareReplacementLifecycle struct {
	variantRevisionID string
	launchID          string
	saveID            string
	payloadBlobIDs    []string
}

func seedFirmwareReplacementLifecycle(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	blobs *blobstore.Store,
	artifactID, installationID, biosBlobID string,
) firmwareReplacementLifecycle {
	t.Helper()
	contentBlobID := ensureFirmwareBlob(t, ctx, database, blobs, []byte("firmware-game-content"))
	stateBlobID := ensureFirmwareBlob(t, ctx, database, blobs, []byte("firmware-save-state"))
	screenshotBlobID := ensureFirmwareBlob(t, ctx, database, blobs, []byte("firmware-save-screenshot"))
	now := time.Now().UnixMilli()
	snapshot := fmt.Sprintf(
		`{"schemaVersion":1,"bios":[{"installationId":%q,"blobId":%q}]}`,
		installationID, biosBlobID,
	)
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO platform_instances(id,platform_id,default_core_id,name,slug,sort_order,enabled,created_at_ms,updated_at_ms)
VALUES('firmware-platform','gba','mgba','Firmware GBA','firmware-gba',0,1,?,?)`, []any{now, now}},
		{`INSERT INTO game_metadata_revisions(id,game_id,title,description,developer,publisher,genre,source_kind,created_at_ms)
VALUES('firmware-metadata','firmware-game','Firmware','','','','','ADMIN_EDIT',?)`, []any{now}},
		{
			`INSERT INTO game_content_revisions(id,game_id,source_kind,source_ref_id,source_manifest_json,
source_manifest_digest,created_at_ms)
VALUES('firmware-content','firmware-game','ADMIN_REPLACE','firmware-source','{}',?,?)`,
			[]any{strings.Repeat("1", 64), now},
		},
		{`INSERT INTO games(id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,
search_text,created_at_ms,updated_at_ms)
VALUES('firmware-game','firmware-platform','PUBLISHED','firmware-metadata','firmware-content','firmware',?,?)`, []any{now, now}},
		{`INSERT INTO game_content_files(game_content_revision_id,role,logical_name,blob_id,sort_order)
VALUES('firmware-content','CONTENT','firmware.gba',?,0)`, []any{contentBlobID}},
		{`INSERT INTO game_variants(id,game_id,core_id,current_revision_id,created_at_ms,updated_at_ms)
VALUES('firmware-variant','firmware-game','mgba',NULL,?,?)`, []any{now, now}},
		{
			`INSERT INTO game_variant_revisions(id,game_variant_id,game_content_revision_id,core_artifact_id,
validation_input_digest,emulator_game_id,status,compatibility_code,dependency_snapshot_json,created_at_ms)
VALUES('firmware-variant-revision','firmware-variant','firmware-content',?, ?,800001,'READY','READY',?,?)`,
			[]any{artifactID, strings.Repeat("2", 64), snapshot, now},
		},
		{`UPDATE game_variants SET current_revision_id='firmware-variant-revision' WHERE id='firmware-variant'`, nil},
		{`INSERT INTO variant_files(game_variant_revision_id,role,logical_name,blob_id,sort_order)
VALUES('firmware-variant-revision','BIOS_BUNDLE','gba_bios.bin',?,0)`, []any{biosBlobID}},
		{`INSERT INTO profiles(id,display_name,created_at_ms) VALUES('firmware-profile','Firmware',?)`, []any{now}},
		{`INSERT INTO launch_sessions(id,profile_id,game_id,game_variant_revision_id,core_artifact_id,
return_to,credential_sha256,state,bootstrap_expires_at_ms,idle_expires_at_ms,activated_at_ms,
hard_expires_at_ms,created_at_ms,updated_at_ms)
VALUES('firmware-launch','firmware-profile','firmware-game','firmware-variant-revision',?,'/',?,'ACTIVE',
?,?,?,?,?,?)`, []any{artifactID, make([]byte, 32), now + 60_000, now + 60_000, now, now + 120_000, now, now}},
		{`INSERT INTO launch_content_files(launch_session_id,logical_name,blob_id,format_version,created_at_ms)
VALUES('firmware-launch','firmware.gba',?,'SOURCE_V1',?)`, []any{contentBlobID, now}},
		{`INSERT INTO save_states(id,profile_id,game_id,game_variant_revision_id,core_artifact_id,
state_blob_id,screenshot_blob_id,name,active_duration_ms,created_at_ms,updated_at_ms,source_launch_session_id)
VALUES('firmware-save','firmware-profile','firmware-game','firmware-variant-revision',?,?,?,
'Firmware save',1,?,?,'firmware-launch')`, []any{artifactID, stateBlobID, screenshotBlobID, now, now}},
		{`UPDATE launch_sessions SET save_state_id='firmware-save' WHERE id='firmware-launch'`, nil},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	return firmwareReplacementLifecycle{
		variantRevisionID: "firmware-variant-revision",
		launchID:          "firmware-launch",
		saveID:            "firmware-save",
		payloadBlobIDs:    []string{stateBlobID, screenshotBlobID},
	}
}

func ensureFirmwareBlob(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	blobs *blobstore.Store,
	contents []byte,
) string {
	t.Helper()
	metadata, err := blobs.Put(bytes.NewReader(contents))
	if err != nil {
		t.Fatal(err)
	}
	blobID, err := blobstore.EnsureRecord(ctx, database, metadata, "application/octet-stream", time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	return blobID
}

func assertFirmwareReplacementLifecycle(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	lifecycle firmwareReplacementLifecycle,
) {
	t.Helper()
	var variantFiles, saves, launchFiles int
	var launchState string
	if err := database.QueryRowContext(ctx, `
SELECT
 (SELECT count(*) FROM variant_files WHERE game_variant_revision_id=?),
 (SELECT count(*) FROM save_states WHERE id=?),
 (SELECT state FROM launch_sessions WHERE id=?),
 (SELECT count(*) FROM launch_content_files WHERE launch_session_id=?)
`, lifecycle.variantRevisionID, lifecycle.saveID, lifecycle.launchID, lifecycle.launchID).
		Scan(&variantFiles, &saves, &launchState, &launchFiles); err != nil {
		t.Fatal(err)
	}
	if variantFiles != 0 || saves != 0 || launchState != "REVOKED" || launchFiles != 0 {
		t.Fatalf(
			"BIOS replacement lifecycle = variant files %d, saves %d, launch %s, launch files %d",
			variantFiles, saves, launchState, launchFiles,
		)
	}
	for _, blobID := range lifecycle.payloadBlobIDs {
		var candidates int
		if err := database.QueryRowContext(
			ctx, `SELECT count(*) FROM blob_gc_candidates WHERE blob_id=?`, blobID,
		).Scan(&candidates); err != nil || candidates != 1 {
			t.Fatalf("BIOS replacement payload %s candidates = %d, error=%v", blobID, candidates, err)
		}
	}
}

func completeFirmwareUpload(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
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
		if err := database.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=?`, jobID).Scan(&state); err != nil {
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
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	var artifactID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id FROM core_artifacts WHERE core_id='mame2003_plus' AND enabled=1 LIMIT 1
`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	contents := []byte("deterministic ST-V BIOS fixture")
	_, sha1Value := legacychecksum.Sum(contents)
	crc32Value := fmt.Sprintf("%08x", crc32.ChecksumIEEE(contents))
	now := time.Now().UnixMilli()
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_versions(id,core_id,core_artifact_id,builtin_relative_path,sha256,parser_version,
parse_status,is_active,machine_count,rom_entry_count,disk_entry_count,bios_set_count,
default_bios_set_count,explicit_bios_machine_count,base_dependency_target_count,unresolved_relation_count,
version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
VALUES('dat-test','mame2003_plus',?,'test.dat',?,'test-parser','READY',1,1,1,0,1,1,1,0,0,1,?,?,?,?)
`, artifactID, strings.Repeat("a", 64), now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_machines(dat_version_id,machine_name,description,year,manufacturer,is_explicit_bios,classification)
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
INSERT INTO bios_requirements(id,core_id,core_artifact_id,source_kind,dat_machine_name,logical_name,
requirement_mode,condition_code,catalog_digest,source_url,source_version,enabled,version,created_at_ms,updated_at_ms)
VALUES('requirement-test','mame2003_plus',?,'DAT_MACHINE','stvbios','stvbios.zip','REQUIRED',
'ARCADE_DAT_DEPENDENCY',?,'retrom:test','dat-test',1,1,?,?)
`, artifactID, strings.Repeat("b", 64), now, now); err != nil {
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
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{SourceType: "FILES", Files: []uploads.FileDeclaration{{ClientFileID: "bios", RelativePath: "stvbios.zip", SizeBytes: int64(archive.Len())}}})
	testassert.False(t, err != nil, err)
	digest := sha256.Sum256(archive.Bytes())
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, fmt.Sprintf("bytes 0-%d/%d", archive.Len()-1, archive.Len()), "sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := uploadService.Get(ctx, upload.ID)
	jobID, _, err := uploadService.Complete(ctx, upload.ID, snapshot.Version)
	testassert.False(t, err != nil, err)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		_ = database.SQL.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=?`, jobID).Scan(&state)
		if state == "SUCCEEDED" {
			break
		}
		testassert.Falsef(t, time.Now().After(deadline), "finalize state = %s", state)
		time.Sleep(10 * time.Millisecond)
	}
	result, err := New(database.SQL, time.Now).WithBlobStore(blobs).Install(
		ctx, "requirement-test", 1, InstallRequest{UploadFileID: upload.Files[0].ID},
	)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return result.Status != "MATCHED" }, func() bool { return !result.Active }), "installation = %#v", result)
	warnings, ok := result.ValidationDetails["warnings"].([]string)
	testassert.Falsef(t, testassert.Any(func() bool { return !ok }, func() bool { return len(warnings) != 1 }, func() bool { return !strings.Contains(warnings[0], "epr-19730.ic8") }), "alias warnings = %#v", result.ValidationDetails["warnings"])
	inspection, err := New(database.SQL, time.Now).InspectArchive(ctx, "requirement-test")
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return inspection.LogicalName != "stvbios.zip" }, func() bool { return inspection.InstallationStatus != "MATCHED" }, func() bool { return len(inspection.Entries) != 2 }), "inspection = %#v", inspection)
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
	if err := database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM archive_entries`).Scan(&indexed); err != nil || indexed != 2 {
		t.Fatalf("archive entries = %d, error=%v", indexed, err)
	}
}
