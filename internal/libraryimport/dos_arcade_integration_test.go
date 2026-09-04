//go:build integration

package libraryimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/importing"
	"retrom/internal/launch"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestDOSDirectoryGroupingProducesDeterministicBundleAndSafePrograms(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	inputs := []struct {
		path     string
		contents []byte
	}{
		{"GAME/INSTALL.BAT", []byte("install")},
		{"GAME/DOOM.EXE", []byte("exe")},
		{"GAME/DATA.WAD", []byte("wad")},
		{"BAD%NAME.BAT", []byte("bat")},
	}
	files := make([]importSourceFile, 0, len(inputs))
	for index, input := range inputs {
		metadata, err := blobs.Put(bytes.NewReader(input.contents))
		testassert.False(t, err != nil, err)
		files = append(
			files,
			importSourceFile{
				id:     fmt.Sprintf("file-%d", index),
				path:   input.path,
				blobID: fmt.Sprintf("blob-%d", index),
				sha256: metadata.SHA256,
			},
		)
	}
	service := (&Service{}).WithBlobStore(blobs)
	dispositions, groups, _ := service.prepareDOSFiles(context.Background(), "DIRECTORY", files)
	testassert.Falsef(t, testassert.Any(func() bool { return len(dispositions) != 4 }, func() bool { return len(groups) != 1 }, func() bool { return len(groups[0].sources) != 4 }, func() bool { return len(groups[0].dosEntries) != 3 }, func() bool { return groups[0].defaultDOSEntry != "GAME/DOOM.EXE" }, func() bool { return groups[0].bundle == nil }), "DOS grouping = dispositions:%d groups:%#v", len(dispositions), groups)
	testassert.Falsef(t, testassert.Any(func() bool { return !groups[0].dosEntries[0].safe }, func() bool { return groups[0].dosEntries[1].safe }, func() bool { return groups[0].dosEntries[2].path != "GAME/INSTALL.BAT" }, func() bool { return groups[0].dosEntries[2].rank != 2 }), "DOS direct safety = %#v", groups[0].dosEntries)
	_, repeated, _ := service.prepareDOSFiles(context.Background(), "DIRECTORY", files)
	testassert.Falsef(t, testassert.Any(func() bool { return len(repeated) != 1 }, func() bool { return repeated[0].bundle == nil }, func() bool { return repeated[0].bundle.SHA256 != groups[0].bundle.SHA256 }), "DOS bundle hash drift = %#v / %#v", groups[0].bundle, repeated)
	multi, noGroups, _ := service.prepareDOSFiles(context.Background(), "FILES", files[:2])
	testassert.Falsef(t, testassert.Any(func() bool { return len(noGroups) != 0 }, func() bool { return len(multi) != 2 }, func() bool { return multi[0].reason != "AMBIGUOUS_DOS_BUNDLE" }, func() bool { return multi[1].reason != "AMBIGUOUS_DOS_BUNDLE" }), "ambiguous DOS files = %#v / %#v", multi, noGroups)
	zipBytes := makeZIP(t, map[string][]byte{"GAME/DOOM.EXE": []byte("exe"), "GAME/DATA.WAD": []byte("wad")})
	zipMetadata, err := blobs.Put(bytes.NewReader(zipBytes))
	testassert.False(t, err != nil, err)
	zipFile := importSourceFile{id: "zip-file", path: "Doom.zip", blobID: "zip-blob", sha256: zipMetadata.SHA256}
	zipDispositions, zipGroups, archives := service.prepareDOSFiles(
		context.Background(),
		"FILES",
		[]importSourceFile{zipFile},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return len(zipDispositions) != 1 }, func() bool { return len(zipGroups) != 1 }, func() bool { return len(zipGroups[0].sources) != 2 }, func() bool { return len(archives) != 1 }, func() bool { return len(archives[0].materialized) != 2 }), "DOS ZIP grouping = dispositions:%#v groups:%#v archives:%#v", zipDispositions, zipGroups, archives)
	for _, source := range zipGroups[0].sources {
		testassert.Falsef(t, testassert.Any(func() bool { return source.role != "DOS_SOURCE" }, func() bool { return source.archiveOrdinal == nil }, func() bool { return source.archiveBlobID != "zip-blob" }), "DOS ZIP source = %#v", source)
	}
	for _, unsafe := range []string{"GAME/100%.BAT", "GAME/QUOTE\".EXE", "GAME/TRAILING .EXE ", "GAME/TRAILING.EXE.", "游戏.EXE"} {
		testassert.Falsef(t, directDOSPathSafe(unsafe), "unsafe DOS path accepted: %q", unsafe)
	}
}

func TestArcadeDraftBIOSStateRefreshesInstalledDATMachineDependency(t *testing.T) {
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
	target, err := testsupport.LookupRuntimeTarget(ctx, database.SQL, "fbneo")
	testassert.False(t, err != nil, err)
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	archive := makeZIP(t, map[string][]byte{"b.bin": []byte("bios")})
	metadata, err := blobs.Put(bytes.NewReader(archive))
	testassert.False(t, err != nil, err)
	blobID, err := blobstore.EnsureRecord(ctx, database.SQL, metadata, "application/zip", time.Now().UnixMilli())
	testassert.False(t, err != nil, err)
	const requirementID = "01990000-0000-7000-8000-000000000101"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO bios_requirements(id,core_id,provider_id,target_id,target_contract_sha256,source_kind,dat_machine_name,logical_name,
requirement_mode,condition_code,activation_options_json,catalog_digest,size_bytes,md5,sha1,sha256,
source_url,source_version,enabled,version,created_at_ms,updated_at_ms,delivery_kind,emulator_path)
VALUES(?,'fbneo',?,?,?,'DAT_MACHINE','bios','bios.zip','REQUIRED','ARCADE_DAT_DEPENDENCY','{}',?,
NULL,NULL,NULL,NULL,'test://bios','test',1,1,?,?,'BIOS_BUNDLE',NULL)
`, requirementID, target.ProviderID, target.TargetID, target.TargetContractSHA256,
		strings.Repeat("a", 64), time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES('01990000-0000-7000-8000-000000000102',?,?,?, ?,?,?,?,1,'MATCHED','{}',1,1,?,?)
`, requirementID, blobID, "bios.zip", metadata.Size, metadata.MD5, metadata.SHA1, metadata.SHA256,
		time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	previous := `{"schemaVersion":2,"machine":"child","datVersionId":"dat-test","closure":["child","bios"],"dependencies":[{"kind":"BIOS_OR_BASE","machine":"bios","state":"MISSING","requiredEntries":["b.bin"]}],"missingEntries":["bios.zip"],"mismatchedEntries":[],"warnings":[]}`
	transaction, err := database.SQL.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Rollback(transaction) })
	resolved, err := resolveArcadeDraftBIOSState(
		ctx, transaction, target.ProviderID, target.TargetID, previous, "BLOCKED", "LAUNCH_BIOS_MISSING",
	)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return !resolved.tracked }, func() bool { return resolved.replaceBundle }, func() bool { return resolved.status != "READY" }, func() bool { return resolved.code != "READY" }, func() bool { return len(resolved.dependencies) != 1 }, func() bool { return resolved.dependencies[0].BlobID == nil }, func() bool { return *resolved.dependencies[0].BlobID != blobID }), "resolved arcade BIOS state = %#v", resolved)
	var snapshot arcadeDraftSnapshot
	if err := json.Unmarshal([]byte(resolved.snapshotJSON), &snapshot); err != nil || len(snapshot.MissingEntries) != 0 ||
		len(snapshot.Dependencies) != 1 || snapshot.Dependencies[0].State != "SATISFIED_EXTERNAL" {
		t.Fatalf("resolved arcade snapshot = %#v, error=%v", snapshot, err)
	}
}

func TestArcadeImportUsesInstalledBIOSBeforeCreatingReview(t *testing.T) {
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
	target, err := testsupport.LookupRuntimeTarget(ctx, database.SQL, "fbneo")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	dummy, err := blobs.Put(bytes.NewReader([]byte("synthetic DAT")))
	testassert.False(t, err != nil, err)
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE dat_versions SET is_active=0,version=version+1,updated_at_ms=?
WHERE provider_id=? AND target_id=? AND is_active=1
`, now, target.ProviderID, target.TargetID); err != nil {
		t.Fatal(err)
	}
	const datID = "01990000-0000-7000-8000-000000000201"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_versions(id,core_id,provider_id,target_id,target_contract_sha256,builtin_relative_path,sha256,parser_version,
parse_status,is_active,machine_count,rom_entry_count,disk_entry_count,
bios_set_count,default_bios_set_count,explicit_bios_machine_count,base_dependency_target_count,
unresolved_relation_count,version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
VALUES(?,'fbneo',?,?,?,'testdata/installed-bios.dat',?,'test','READY',1,2,2,0,0,0,1,1,0,1,?,?,?,?)
`, datID, target.ProviderID, target.TargetID, target.TargetContractSHA256, dummy.SHA256, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_machines(dat_version_id,machine_name,description,year,manufacturer,cloneof,romof,
is_explicit_bios,classification) VALUES
(?,'codexchild','Child','','',NULL,'codexbios',0,'NORMAL'),
(?,'codexbios','BIOS','','',NULL,NULL,1,'EXPLICIT_BIOS')
`, datID, datID); err != nil {
		t.Fatal(err)
	}
	childArchive := makeZIP(t, map[string][]byte{"c.bin": []byte("child")})
	biosArchive := makeZIP(t, map[string][]byte{"b.bin": []byte("bios")})
	type archiveRecord struct {
		machine string
		entry   string
		body    []byte
		bytes   []byte
	}
	for _, fixture := range []archiveRecord{
		{machine: "codexchild", entry: "c.bin", body: []byte("child"), bytes: childArchive},
		{machine: "codexbios", entry: "b.bin", body: []byte("bios"), bytes: biosArchive},
	} {
		metadata, putErr := blobs.Put(bytes.NewReader(fixture.bytes))
		testassert.False(t, putErr != nil, putErr)
		entries, scanErr := importing.ScanZIP(ctx, blobs.Path(metadata.SHA256), importing.DefaultArchiveLimits())
		testassert.Falsef(t, testassert.Any(func() bool { return scanErr != nil }, func() bool { return len(entries) != 1 }), "scan %s = %#v, error=%v", fixture.machine, entries, scanErr)
		if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_rom_entries(dat_version_id,machine_name,ordinal,name,size_bytes,crc32,sha1,status)
VALUES(?,?,0,?,?,?,?,'GOOD')
`, datID, fixture.machine, fixture.entry, len(fixture.body), entries[0].CRC32, entries[0].SHA1); err != nil {
			t.Fatal(err)
		}
	}
	biosMetadata, err := blobs.Put(bytes.NewReader(biosArchive))
	testassert.False(t, err != nil, err)
	biosBlobID, err := blobstore.EnsureRecord(ctx, database.SQL, biosMetadata, "application/zip", now)
	testassert.False(t, err != nil, err)
	const requirementID = "01990000-0000-7000-8000-000000000202"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO bios_requirements(id,core_id,provider_id,target_id,target_contract_sha256,source_kind,dat_machine_name,logical_name,
requirement_mode,condition_code,activation_options_json,catalog_digest,size_bytes,md5,sha1,sha256,
source_url,source_version,enabled,version,created_at_ms,updated_at_ms,delivery_kind,emulator_path)
VALUES(?,'fbneo',?,?,?,'DAT_MACHINE','codexbios','codexbios.zip','REQUIRED',
'ARCADE_DAT_DEPENDENCY','{}',?,NULL,NULL,NULL,NULL,'test://bios',?,1,1,?,?,'BIOS_BUNDLE',NULL)
`, requirementID, target.ProviderID, target.TargetID, target.TargetContractSHA256,
		strings.Repeat("a", 64), datID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES('01990000-0000-7000-8000-000000000203',?,?,?, ?,?,?,?,1,'MATCHED','{}',1,1,?,?)
`, requirementID, biosBlobID, "codexbios.zip", biosMetadata.Size, biosMetadata.MD5, biosMetadata.SHA1,
		biosMetadata.SHA256, now, now); err != nil {
		t.Fatal(err)
	}
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{
		SourceType: "FILES",
		Files: []uploads.FileDeclaration{{
			ClientFileID: "child", RelativePath: "codexchild.zip", SizeBytes: int64(len(childArchive)),
		}},
	})
	testassert.False(t, err != nil, err)
	digest := sha256.Sum256(childArchive)
	if err := uploadService.PutPart(
		ctx,
		upload.ID,
		upload.Files[0].ID,
		0,
		fmt.Sprintf("bytes 0-%d/%d", len(childArchive)-1, len(childArchive)),
		"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":",
		bytes.NewReader(childArchive),
	); err != nil {
		t.Fatal(err)
	}
	current, err := uploadService.Get(ctx, upload.ID)
	testassert.False(t, err != nil, err)
	jobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	testassert.False(t, err != nil, err)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		if err := database.SQL.QueryRowContext(ctx, "SELECT state FROM jobs WHERE id=?", jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			break
		}
		testassert.Falsef(t, time.Now().After(deadline), "upload finalization = %s", state)
		time.Sleep(10 * time.Millisecond)
	}
	importService := New(database.SQL, time.Now).WithBlobStore(blobs)
	created, err := importService.Create(ctx, CreateRequest{
		UploadID:                 upload.ID,
		TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "arcade/fbneo"),
		MetadataProvider:         "NONE",
	})
	testassert.False(t, err != nil, err)
	var validationID, status, code, snapshotJSON string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT draft.selected_validation_id,validation.status,validation.compatibility_code,
validation.dependency_snapshot_json
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_core_validations validation ON validation.id=draft.selected_validation_id
WHERE item.import_job_id=?
`, created.ImportJobID).Scan(&validationID, &status, &code, &snapshotJSON); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return status != "READY" }, func() bool { return code != "READY" }), "initial validation = %s/%s", status, code)
	var snapshot arcadeDraftSnapshot
	if err := json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil || len(snapshot.MissingEntries) != 0 ||
		len(snapshot.Dependencies) != 1 || snapshot.Dependencies[0].State != "SATISFIED_EXTERNAL" {
		t.Fatalf("initial snapshot = %#v, error=%v", snapshot, err)
	}
	var validationBlobID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT blob_id FROM import_item_validation_files
WHERE import_item_core_validation_id=? AND role='BIOS_BUNDLE' AND logical_name='codexbios.zip'
`, validationID).Scan(&validationBlobID); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, validationBlobID != biosBlobID, "initial BIOS blob = %s, want %s", validationBlobID, biosBlobID)
	var itemID string
	var draftVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT item.id,draft.version
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
WHERE item.import_job_id=?
	`, created.ImportJobID).Scan(&itemID, &draftVersion); err != nil {
		t.Fatal(err)
	}
	approved, err := importService.Approve(ctx, itemID, draftVersion)
	testassert.False(t, err != nil, err)
	replacementMetadata, err := blobs.Put(bytes.NewReader(append(biosArchive, []byte("replacement")...)))
	testassert.False(t, err != nil, err)
	replacementBlobID, err := blobstore.EnsureRecord(
		ctx, database.SQL, replacementMetadata, "application/zip", now,
	)
	testassert.False(t, err != nil, err)
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE bios_installations SET is_active=0,version=version+1,updated_at_ms=?
WHERE requirement_id=? AND is_active=1
`, now+1, requirementID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES('01990000-0000-7000-8000-000000000204',?,?,?, ?,?,?,?,1,'HASH_WARNING','{}',1,1,?,?)
`, requirementID, replacementBlobID, "codexbios.zip", replacementMetadata.Size, replacementMetadata.MD5,
		replacementMetadata.SHA1, replacementMetadata.SHA256, now+1, now+1); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('local','Arcade BIOS Fixture',?)
`, now); err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	testassert.False(t, err != nil, err)
	runtimeBuilder, err := testsupport.NewRuntimeBuilder(ctx, database.SQL)
	testassert.False(t, err != nil, err)
	launcher := launch.New(database.SQL, dependencySet, credentials, time.Now).
		WithRuntimeProvider(dependencySet.RuntimeCatalog, runtimeBuilder)
	coreID := "fbneo"
	capabilities := launch.Capabilities{
		SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true,
	}
	pending, err := launcher.Create(ctx, "local", launch.CreateRequest{
		GameID: approved.GameID, CoreID: &coreID, ReturnTo: "/games/" + approved.GameID,
		ClientCapabilities: capabilities,
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return pending.Status != "VALIDATION_PENDING" }, func() bool { return pending.JobID == "" }), "Arcade BIOS revalidation = %#v, error=%v", pending, err)
	deadline = time.Now().Add(3 * time.Second)
	for {
		var state string
		var errorCode sql.NullString
		if err := database.SQL.QueryRowContext(ctx, `SELECT state,error_code FROM jobs WHERE id=?`, pending.JobID).
			Scan(&state, &errorCode); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			break
		}
		testassert.Falsef(t, testassert.Any(func() bool { return state == "FAILED" }, func() bool { return time.Now().After(deadline) }), "Arcade BIOS revalidation = %s/%s", state, errorCode.String)
		time.Sleep(10 * time.Millisecond)
	}
	var refreshedSnapshot string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT revision.dependency_snapshot_json
FROM game_variants variant
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
WHERE variant.game_id=? AND variant.core_id='fbneo'
`, approved.GameID).Scan(&refreshedSnapshot); err != nil {
		t.Fatal(err)
	}
	var refreshedEnvelope struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal([]byte(refreshedSnapshot), &refreshedEnvelope); err != nil || refreshedEnvelope.SchemaVersion != 2 {
		t.Fatalf("refreshed Arcade dependency snapshot = %s, error=%v", refreshedSnapshot, err)
	}
	createdLaunch, err := launcher.Create(ctx, "local", launch.CreateRequest{
		GameID: approved.GameID, CoreID: &coreID, ReturnTo: "/games/" + approved.GameID,
		ClientCapabilities: capabilities,
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return createdLaunch.LaunchID == "" }), "Arcade BIOS launch after revalidation = %#v, error=%v", createdLaunch, err)
	configuration, err := launcher.Config(ctx, createdLaunch.LaunchID, createdLaunch.Capability)
	testassert.False(t, err != nil, err)
	envelope := testsupport.RuntimeEnvelope(t, configuration)
	bios := testsupport.RuntimeEnvelopeResource(t, envelope, "bios")
	testassert.Falsef(t, bios["kind"] != "BIOS_BUNDLE", "Arcade BIOS launch resource = %#v", bios)
	bundle, err := launcher.BundleFiles(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "BIOS_BUNDLE")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(bundle) != 1 }, func() bool { return bundle[0].LogicalName != "codexbios.zip" }, func() bool { return bundle[0].SHA256 != replacementMetadata.SHA256 }), "Arcade BIOS launch bundle = %#v, error=%v", bundle, err)
}
