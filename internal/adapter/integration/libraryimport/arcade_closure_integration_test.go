//go:build integration

package libraryimport

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	firmwaresource "retrom/internal/adapter/content/firmwaremanifest"

	dependencypersistence "retrom/internal/repo/dependencies"
	dependencyservice "retrom/internal/service/dependencies"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/runtime/dependencies"
	"retrom/internal/capability/format/importing"
	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/store"
	"retrom/internal/testkit/testassert"
	"retrom/internal/testkit/testsupport"
)

func TestArcadeGroupingBuildsCoreScopedParentAndBIOSClosure(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newArcadeGroupingFixture(ctx, t)
	fixture.assertExternalDependencies(ctx, t)
	fixture.assertSelfContainedGroups(ctx, t)
	fixture.assertMergedGroups(ctx, t)
	fixture.assertUnsupportedDisk(ctx, t)
}

type arcadeGroupingFixture struct {
	database *store.DB
	blobs    *blobstore.Store
	service  *Service
	datID    string
	files    []importSourceFile
}

func newArcadeGroupingFixture(ctx context.Context, t *testing.T) arcadeGroupingFixture {
	t.Helper()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencyservice.New(dependencySet, dependencypersistence.New(database.SQL), firmwaresource.Source{}).Bootstrap(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	dummy, err := blobs.Put(bytes.NewReader([]byte("synthetic dat")))
	testassert.False(t, err != nil, err)
	target, err := testsupport.LookupRuntimeTarget(ctx, database.SQL, "fbneo")
	if err != nil {
		t.Fatal(err)
	}
	datID := "01980000-0000-7000-8000-000000000201"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_versions(id,
core_id,
provider_id,
target_id,
builtin_relative_path,
sha256,
parser_version,
parse_status,
is_active,
machine_count,
rom_entry_count,
disk_entry_count,
bios_set_count,
default_bios_set_count,
explicit_bios_machine_count,
base_dependency_target_count,
unresolved_relation_count,
version,
created_at_ms,
updated_at_ms,
parsed_at_ms) VALUES(?,
	'fbneo',
	?,
	?,
	'testdata/grouping.dat',
?,
'test',
'READY',
0,
3,
3,
0,
0,
0,
1,
1,
0,
1,
?,
?,
?)
`,
		datID,
		target.ProviderID,
		target.TargetID,
		dummy.SHA256,
		time.Now().UnixMilli(),
		time.Now().UnixMilli(),
		time.Now().UnixMilli(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_machines(dat_version_id,
machine_name,
description,
year,
manufacturer,
cloneof,
romof,
is_explicit_bios,
classification) VALUES
(?,
'child',
'Child',
'',
'',
'parent',
'bios',
0,
'NORMAL'),
(?,
'parent',
'Parent',
'',
'',
NULL,
'bios',
0,
'NORMAL'),
(?,
'bios',
'BIOS',
'',
'',
NULL,
NULL,
1,
'EXPLICIT_BIOS')
`, datID, datID, datID); err != nil {
		t.Fatal(err)
	}

	fixture := arcadeGroupingFixture{database: database, blobs: blobs, datID: datID, service: (&Service{database: database.SQL}).WithBlobStore(blobs)}
	fixture.files = fixture.createArchives(ctx, t)
	return fixture
}

func (fixture arcadeGroupingFixture) createArchives(ctx context.Context, t *testing.T) []importSourceFile {
	t.Helper()
	database, blobs, datID := fixture.database, fixture.blobs, fixture.datID
	type archiveFixture struct {
		id, name, entry string
		body            []byte
		file            importSourceFile
	}
	fixtures := []archiveFixture{
		{id: "child-file", name: "child.zip", entry: "c.bin", body: []byte("child")},
		{id: "parent-file", name: "parent.zip", entry: "p.bin", body: []byte("parent")},
		{id: "bios-file", name: "bios.zip", entry: "b.bin", body: []byte("bios")},
	}
	for index := range fixtures {
		archiveBytes := makeZIP(t, map[string][]byte{fixtures[index].entry: fixtures[index].body})
		metadata, putErr := blobs.Put(bytes.NewReader(archiveBytes))
		testassert.False(t, putErr != nil, putErr)
		fixtures[index].file = importSourceFile{
			ID:     fixtures[index].id,
			Path:   fixtures[index].name,
			BlobID: "blob-" + fixtures[index].id,
			SHA256: metadata.SHA256,
		}
		entryDigest := sha256.Sum256(fixtures[index].body)
		entries, scanErr := importing.ScanZIP(ctx, blobs.Path(metadata.SHA256), importing.DefaultArchiveLimits())
		testassert.Falsef(t, testassert.Any(func() bool { return scanErr != nil }, func() bool { return len(entries) != 1 }), "scan %s = %#v, error=%v", fixtures[index].name, entries, scanErr)
		machine := strings.TrimSuffix(fixtures[index].name, ".zip")
		if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_rom_entries(dat_version_id,
machine_name,
ordinal,
name,
size_bytes,
crc32,
sha1,
status) VALUES(?,
?,
?,
?,
?,
?,
?,
'GOOD')
`,
			datID,
			machine,
			0,
			fixtures[index].entry,
			len(fixtures[index].body),
			entries[0].CRC32,
			entries[0].SHA1,
		); err != nil {
			t.Fatalf("insert ROM %x: %v", entryDigest, err)
		}
	}

	return []importSourceFile{fixtures[0].file, fixtures[1].file, fixtures[2].file}
}

func (fixture arcadeGroupingFixture) assertExternalDependencies(ctx context.Context, t *testing.T) {
	t.Helper()
	service, files, datID := fixture.service, fixture.files, fixture.datID
	dispositions, groups, archives, preparationErr := service.prepareArcadeFiles(ctx, files, sql.NullString{String: datID, Valid: true})
	if preparationErr != nil {
		t.Fatal(preparationErr)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(dispositions) != 3 }, func() bool { return len(groups) != 1 }, func() bool { return len(archives) != 3 }), "arcade grouping counts = dispositions:%#v groups:%#v archives:%d", dispositions, groups, len(archives))
	child := groups[0]
	testassert.Falsef(t, testassert.Any(func() bool { return child.ValidationStatus != "READY" }, func() bool { return len(child.Sources) != 3 }, func() bool { return len(child.ValidationFiles) != 2 }, func() bool { return child.ValidationFiles[0].Role != "PARENT" }, func() bool { return child.ValidationFiles[1].Role != "BIOS_BUNDLE" }), "child dependency closure = %#v", child)
	for _, disposition := range dispositions {
		testassert.Falsef(t, disposition.Disposition != "SOURCE", "referenced archive disposition = %#v", disposition)
	}
	missingDispositions, missingGroups, _, preparationErr := service.prepareArcadeFiles(
		ctx,
		[]importSourceFile{files[0]},
		sql.NullString{String: datID, Valid: true},
	)
	if preparationErr != nil {
		t.Fatal(preparationErr)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(missingDispositions) != 1 }, func() bool { return len(missingGroups) != 1 }, func() bool { return missingGroups[0].ValidationStatus != "BLOCKED" }, func() bool {
		return missingGroups[0].CompatibilityCode != "LAUNCH_BIOS_MISSING" &&
			missingGroups[0].CompatibilityCode != "LAUNCH_PARENT_MISSING"
	}), "missing dependency = dispositions:%#v groups:%#v", missingDispositions, missingGroups)
}

func (fixture arcadeGroupingFixture) assertSelfContainedGroups(ctx context.Context, t *testing.T) {
	t.Helper()
	service, blobs, datID := fixture.service, fixture.blobs, fixture.datID
	fullBytes := makeZIP(
		t,
		map[string][]byte{"c.bin": []byte("child"), "p.bin": []byte("parent"), "b.bin": []byte("bios")},
	)
	fullMetadata, err := blobs.Put(bytes.NewReader(fullBytes))
	testassert.False(t, err != nil, err)
	_, fullGroups, _, preparationErr := service.prepareArcadeFiles(
		ctx,
		[]importSourceFile{{ID: "full", Path: "child.zip", BlobID: "full-blob", SHA256: fullMetadata.SHA256}},
		sql.NullString{String: datID, Valid: true},
	)
	if preparationErr != nil {
		t.Fatal(preparationErr)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(fullGroups) != 1 }, func() bool { return fullGroups[0].ValidationStatus != "READY" }, func() bool { return len(fullGroups[0].Sources) != 1 }, func() bool { return len(fullGroups[0].ValidationFiles) != 0 }), "full non-merged closure = %#v", fullGroups)
	fullWithCloneExtraBytes := makeZIP(
		t,
		map[string][]byte{
			"c.bin":       []byte("child"),
			"p.bin":       []byte("parent"),
			"b.bin":       []byte("bios"),
			"clone/c.bin": []byte("clone-specific alternate"),
		},
	)
	fullWithCloneExtraMetadata, err := blobs.Put(bytes.NewReader(fullWithCloneExtraBytes))
	testassert.False(t, err != nil, err)
	_, fullWithCloneExtraGroups, _, preparationErr := service.prepareArcadeFiles(
		ctx,
		[]importSourceFile{{
			ID: "full-with-clone-extra", Path: "child.zip", BlobID: "full-with-clone-extra-blob",
			SHA256: fullWithCloneExtraMetadata.SHA256,
		}},
		sql.NullString{String: datID, Valid: true},
	)
	if preparationErr != nil {
		t.Fatal(preparationErr)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(fullWithCloneExtraGroups) != 1 }, func() bool { return fullWithCloneExtraGroups[0].ValidationStatus != "READY" }, func() bool { return fullWithCloneExtraGroups[0].CompatibilityCode != "READY" }), "full non-merged closure with clone extra = %#v", fullWithCloneExtraGroups)
}

func (fixture arcadeGroupingFixture) assertMergedGroups(ctx context.Context, t *testing.T) {
	t.Helper()
	service, blobs, datID := fixture.service, fixture.blobs, fixture.datID
	mergedBytes := makeZIP(t, map[string][]byte{"c.bin": []byte("child"), "parent/p.bin": []byte("parent")})
	mergedMetadata, err := blobs.Put(bytes.NewReader(mergedBytes))
	testassert.False(t, err != nil, err)
	_, mergedGroups, _, preparationErr := service.prepareArcadeFiles(
		ctx,
		[]importSourceFile{{ID: "merged", Path: "child.zip", BlobID: "merged-blob", SHA256: mergedMetadata.SHA256}},
		sql.NullString{String: datID, Valid: true},
	)
	if preparationErr != nil {
		t.Fatal(preparationErr)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(mergedGroups) != 1 }, func() bool { return mergedGroups[0].ValidationStatus != "BLOCKED" }, func() bool { return mergedGroups[0].CompatibilityCode != "UNSUPPORTED_MERGED_ROMSET" }), "merged ROM set = %#v", mergedGroups)
	nestedMismatchBytes := makeZIP(
		t,
		map[string][]byte{"c.bin": []byte("child"), "parent/p.bin": []byte("not-the-parent-rom")},
	)
	nestedMismatchMetadata, err := blobs.Put(bytes.NewReader(nestedMismatchBytes))
	testassert.False(t, err != nil, err)
	_, nestedMismatchGroups, _, preparationErr := service.prepareArcadeFiles(
		ctx,
		[]importSourceFile{{
			ID: "nested-mismatch", Path: "child.zip", BlobID: "nested-mismatch-blob",
			SHA256: nestedMismatchMetadata.SHA256,
		}},
		sql.NullString{String: datID, Valid: true},
	)
	if preparationErr != nil {
		t.Fatal(preparationErr)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(nestedMismatchGroups) != 1 }, func() bool { return nestedMismatchGroups[0].ValidationStatus != "BLOCKED" }, func() bool { return nestedMismatchGroups[0].CompatibilityCode != "LAUNCH_PARENT_MISSING" }), "nested name-only match must remain a missing parent = %#v", nestedMismatchGroups)
}

func (fixture arcadeGroupingFixture) assertUnsupportedDisk(ctx context.Context, t *testing.T) {
	t.Helper()
	service, database, files, datID := fixture.service, fixture.database, fixture.files, fixture.datID
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_disk_entries(dat_version_id,
machine_name,
ordinal,
name,
sha1,
status) VALUES(?,
'child',
0,
'disk',
?,
'GOOD')
`, datID, strings.Repeat("0", 40)); err != nil {
		t.Fatal(err)
	}
	_, diskGroups, _, preparationErr := service.prepareArcadeFiles(ctx, files, sql.NullString{String: datID, Valid: true})
	if preparationErr != nil {
		t.Fatal(preparationErr)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(diskGroups) == 0 }, func() bool { return diskGroups[0].ValidationStatus != "INCOMPATIBLE" }, func() bool { return diskGroups[0].CompatibilityCode != "UNSUPPORTED_CHD" }), "CHD compatibility = %#v", diskGroups)
}

func makeZIP(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var contents bytes.Buffer
	writer := zip.NewWriter(&contents)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetMode(0o600)
		header.Modified = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
		part, err := writer.CreateHeader(header)
		testassert.False(t, err != nil, err)
		if _, err := part.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return contents.Bytes()
}

func waitForJob(ctx context.Context, t *testing.T, database *store.DB, jobID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		if err := database.SQL.QueryRowContext(ctx, `
SELECT state
FROM jobs
WHERE id=?
`, jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			return
		}
		testassert.Falsef(t, testassert.Any(func() bool { return state == "FAILED" }, func() bool { return time.Now().After(deadline) }), "job %s state = %s", jobID, state)
		time.Sleep(10 * time.Millisecond)
	}
}
