package serverimport

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/firmware"
	"retrom/internal/firmwaremanifest"
	"retrom/internal/legacychecksum"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/serversource"
	"retrom/internal/testsupport"
)

func TestServerArchiveImportFreezesMembersAndRecoversEvaluations(t *testing.T) {
	for _, complete := range []bool{true, false} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			testServerArchiveImport(t, complete)
		})
	}
}

func archiveImportFixture(t *testing.T) (*Service, *sql.DB, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	root := filepath.Join(dir, "source")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	blobs, err := blobstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dir)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := testsupport.LookupRuntimeTarget(ctx, database.SQL, "same_cdi")
	if err != nil {
		t.Fatal(err)
	}
	contents := []byte("Retrom test firmware")
	_, sha := legacychecksum.Sum(contents)
	members, err := json.Marshal([]firmwaremanifest.Member{{Name: "boot.rom", SizeBytes: int64(len(contents)), CRC32: fmt.Sprintf("%08x", crc32.ChecksumIEEE(contents)), SHA1: sha, Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.SQL.ExecContext(ctx, `INSERT INTO bios_requirements(id,core_id,provider_id,target_id,source_kind,logical_name,requirement_mode,catalog_digest,source_url,source_version,enabled,version,created_at_ms,updated_at_ms,delivery_kind,emulator_path,archive_members_json)
VALUES('archive-fixture','same_cdi',?,?,'STATIC','fixture.zip','REQUIRED',?,'retrom:test','fixture-v1',1,1,1,1,'EXTERNAL_FILE','/same_cdi/bios/fixture.zip',?)`, identity.ProviderID, identity.TargetID, strings.Repeat("a", 64), string(members))
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.SQL.ExecContext(ctx, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('01980000-0000-7000-8000-00000000a001','Admin',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-00000000b001','01980000-0000-7000-8000-00000000a001','server.admin','Admin','ADMIN','ENABLED',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, blobs, firmware.New(database.SQL, time.Now).WithBlobStore(blobs), credentials,
		[]serversource.Root{{ID: "bios-root", Label: "BIOS", Path: root}}, time.Now)
	t.Cleanup(service.Close)
	return service, database.SQL, root
}

func writeArchiveCandidate(t *testing.T, root string, complete bool) {
	t.Helper()
	file, err := os.Create(filepath.Join(root, "fixture.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	writer := zip.NewWriter(file)
	name := "boot.rom"
	if !complete {
		name = "unrelated.rom"
	}
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	contents := "Retrom test firmware"
	if !complete {
		contents = "unrelated bytes"
	}
	if _, err = entry.Write([]byte(contents)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertRecoveredArchive(t *testing.T, service *Service, importID string, complete bool) {
	t.Helper()
	ctx := context.Background()

	items, err := service.loadItems(ctx, importID)
	if err != nil || len(items) != 1 || items[0].ArchiveMembersJSON == nil {
		t.Fatalf("frozen items=%#v/%v", items, err)
	}
	recovered, err := service.loadPersistedCandidates(ctx, importID, items)
	if err != nil {
		t.Fatal(err)
	}
	values := recovered[items[0].RequirementID]
	if len(values) != 1 || values[0].DAT == nil || values[0].DAT.Launchable != complete {
		t.Fatalf("recovered=%#v", values)
	}
}

func testServerArchiveImport(t *testing.T, complete bool) {
	t.Helper()

	ctx := context.Background()
	service, database, root := archiveImportFixture(t)
	writeArchiveCandidate(t, root, complete)
	created, err := service.Create(ctx, CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, "01980000-0000-7000-8000-00000000b001")
	if err != nil {
		t.Fatal(err)
	}
	unit, ok, err := service.claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim=%t/%v", ok, err)
	}
	service.execute(ctx, unit)
	var state string
	if err := database.QueryRowContext(ctx, `SELECT state FROM server_bios_import_items WHERE server_import_id=?`, created.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	want := "IMPORTED_MATCHED"
	if !complete {
		want = "INVALID_ARCHIVE"
	}
	if state != want {
		t.Fatalf("state=%s want=%s", state, want)
	}
	assertRecoveredArchive(t, service, created.ID, complete)
	var installed int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM bios_installations WHERE is_active=1`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	wantInstalled := 0
	if complete {
		wantInstalled = 1
	}
	if installed != wantInstalled {
		t.Fatalf("installed=%d", installed)
	}
}
