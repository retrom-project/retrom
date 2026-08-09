//go:build integration

package firmware

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // DAT compatibility checksum, not a security primitive.
	"crypto/sha256"
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
	"retrom/internal/store"
	"retrom/internal/uploads"
)

func TestStaticBIOSHashMismatchIsInstalledAsWarning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)), "sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := uploadService.Get(ctx, upload.ID)
	jobID, _, err := uploadService.Complete(ctx, upload.ID, snapshot.Version)
	if err != nil {
		t.Fatal(err)
	}
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
		if time.Now().After(deadline) {
			t.Fatalf("finalize state = %s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var requirementID string
	var version int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id,
version
FROM bios_requirements
WHERE core_id='mgba'
AND logical_name='gba_bios.bin'
AND enabled=1
`).Scan(&requirementID, &version); err != nil {
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
	result, err := New(
		database.SQL,
		time.Now,
	).Install(ctx, requirementID, version, InstallRequest{UploadFileID: upload.Files[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "HASH_WARNING" || !result.Active {
		t.Fatalf("installation = %#v", result)
	}
}

func TestDATMachineBIOSScansUploadAndAcceptsContentMatchedFilenameAlias(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
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
	sha1Value := fmt.Sprintf("%x", sha1.Sum(contents))
	crc32Value := fmt.Sprintf("%08x", crc32.ChecksumIEEE(contents))
	now := time.Now().UnixMilli()
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_versions(id,core_id,core_artifact_id,source,builtin_relative_path,sha256,parser_version,
compatibility_status,parse_status,is_active,machine_count,rom_entry_count,disk_entry_count,bios_set_count,
default_bios_set_count,explicit_bios_machine_count,base_dependency_target_count,unresolved_relation_count,
version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
VALUES('dat-test','mame2003_plus',?,'BUILTIN','test.dat',?,'test-parser','MATCHED','READY',1,1,1,0,1,1,1,0,0,1,?,?,?,?)
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
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(contents); err != nil {
		t.Fatal(err)
	}
	extraEntry, err := writer.Create("notes/extra.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := extraEntry.Write([]byte("extra")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{SourceType: "FILES", Files: []uploads.FileDeclaration{{ClientFileID: "bios", RelativePath: "stvbios.zip", SizeBytes: int64(archive.Len())}}})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive.Bytes())
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, fmt.Sprintf("bytes 0-%d/%d", archive.Len()-1, archive.Len()), "sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := uploadService.Get(ctx, upload.ID)
	jobID, _, err := uploadService.Complete(ctx, upload.ID, snapshot.Version)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		_ = database.SQL.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=?`, jobID).Scan(&state)
		if state == "SUCCEEDED" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("finalize state = %s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	result, err := New(database.SQL, time.Now).WithBlobStore(blobs).Install(
		ctx, "requirement-test", 1, InstallRequest{UploadFileID: upload.Files[0].ID},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "MATCHED" || !result.Active {
		t.Fatalf("installation = %#v", result)
	}
	warnings, ok := result.ValidationDetails["warnings"].([]string)
	if !ok || len(warnings) != 1 || !strings.Contains(warnings[0], "epr-19730.ic8") {
		t.Fatalf("alias warnings = %#v", result.ValidationDetails["warnings"])
	}
	inspection, err := New(database.SQL, time.Now).InspectArchive(ctx, "requirement-test")
	if err != nil {
		t.Fatal(err)
	}
	if inspection.LogicalName != "stvbios.zip" || inspection.InstallationStatus != "MATCHED" ||
		len(inspection.Entries) != 2 {
		t.Fatalf("inspection = %#v", inspection)
	}
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
