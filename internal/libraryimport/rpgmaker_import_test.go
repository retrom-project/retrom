package libraryimport

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/blobstore"
	"retrom/internal/importing"
	"retrom/internal/rpgmaker/detector"
)

func TestPrepareRPGMakerDirectoryKeepsOneNormalizedProject(t *testing.T) {
	t.Parallel()
	service, files := rpgMakerMVImportFixture(t)
	dispositions, groups, archives, err := service.prepareRPGMakerProject(
		context.Background(), "DIRECTORY", files, "rpgmaker_mv",
	)
	if err != nil {
		t.Fatalf("prepareRPGMakerProject() error = %v", err)
	}
	assertPreparedMVProject(t, groups, archives)
	ignored := countIgnoredDispositions(dispositions)
	if ignored != 2 {
		t.Fatalf("ignored dispositions = %d, want desktop wrapper + packaging noise", ignored)
	}
}

func assertPreparedMVProject(t *testing.T, groups []preparedGroup, archives []preparedArchive) {
	t.Helper()
	if len(groups) != 1 || len(archives) != 0 || groups[0].contentKind != "RPG_MAKER_PROJECT_V1" ||
		groups[0].rpgProfile == nil || groups[0].rpgProfile.ExpectedGeneration != detector.RPGMV ||
		groups[0].rpgProjectRoot != "www" || groups[0].titleSource != "Export" ||
		len(groups[0].sources) != 10 {
		t.Fatalf("prepared group = %#v, archives=%d", groups, len(archives))
	}
	for _, source := range groups[0].sources {
		if source.role != "PROJECT_FILE" || filepath.ToSlash(source.logicalName) == "" ||
			len(source.logicalName) >= 4 && source.logicalName[:4] == "www/" {
			t.Fatalf("project source = %#v", source)
		}
	}
}

func countIgnoredDispositions(dispositions []preparedDisposition) int {
	count := 0
	for _, disposition := range dispositions {
		if disposition.disposition == "IGNORED" {
			count++
		}
	}
	return count
}

func TestRPGMakerDirectoryTitleRequiresOneSharedTopLevelDirectory(t *testing.T) {
	t.Parallel()
	if title := rpgMakerDirectoryTitle([]importSourceFile{
		{path: "Dungeon/index.html"}, {path: "Dungeon/data/System.json"},
	}); title != "Dungeon" {
		t.Fatalf("shared directory title = %q", title)
	}
	if title := rpgMakerDirectoryTitle([]importSourceFile{
		{path: "index.html"}, {path: "data/System.json"},
	}); title != "" {
		t.Fatalf("root project title = %q, want empty", title)
	}
}

func TestPrepareRPGMakerDirectoryRejectsSelectedCoreMismatch(t *testing.T) {
	t.Parallel()
	service, files := rpgMakerMVImportFixture(t)
	_, _, _, err := service.prepareRPGMakerProject(
		context.Background(), "DIRECTORY", files, "rpgmaker_mz",
	)
	var detectionError *detector.Error
	if !errors.As(err, &detectionError) || detectionError.Code != detector.CodeSelectedCoreMismatch {
		t.Fatalf("mismatch error = %v", err)
	}
}

func TestPrepareRPGMakerDirectoryKeepsOpaqueNestedProjectFile(t *testing.T) {
	t.Parallel()
	service, files := rpgMakerMVImportFixture(t)
	files = appendRPGMakerFixtureFile(
		t, service, files, "Export/www/audio/bgm/config",
		[]byte("7z\xbc\xaf\x27\x1c encrypted MTool sidecar"),
	)
	dispositions, groups, _, err := service.prepareRPGMakerProject(
		context.Background(), "DIRECTORY", files, "rpgmaker_mv",
	)
	if err != nil {
		t.Fatal(err)
	}
	if countIgnoredDispositions(dispositions) != 2 || len(groups) != 1 || len(groups[0].sources) != 11 {
		t.Fatalf("dispositions=%#v group=%#v", dispositions, groups)
	}
	found := false
	for _, source := range groups[0].sources {
		if source.logicalName == "audio/bgm/config" {
			found = true
		}
	}
	if !found {
		t.Fatalf("opaque nested project file missing: %#v", groups[0].sources)
	}
}

func TestPrepareRPGMakerDirectoryKeepsOpaqueNativeProjectFiles(t *testing.T) {
	t.Parallel()
	service, files := rpgMakerMVImportFixture(t)
	files = appendRPGMakerFixtureFile(
		t, service, files, "Export/www/plugin.node", []byte("opaque native payload"),
	)
	dispositions, groups, _, err := service.prepareRPGMakerProject(
		context.Background(), "DIRECTORY", files, "rpgmaker_mv",
	)
	if err != nil {
		t.Fatal(err)
	}
	if countIgnoredDispositions(dispositions) != 2 || len(groups) != 1 || len(groups[0].sources) != 11 {
		t.Fatalf("dispositions=%#v group=%#v", dispositions, groups)
	}
	for _, source := range groups[0].sources {
		if source.logicalName == "plugin.node" && source.role == "PROJECT_FILE" {
			return
		}
	}
	t.Fatalf("opaque native project file missing: %#v", groups[0].sources)
}

func TestPrepareRPGMakerRootProjectPreservesDesktopPayloadInSourceFiles(t *testing.T) {
	t.Parallel()
	service, files := rpgMakerMVImportFixture(t)
	for index := range files {
		files[index].path = strings.Replace(files[index].path, "Export/www/", "Export/", 1)
	}
	for _, file := range []struct {
		name string
		body []byte
	}{
		{name: "Export/nw.dll", body: []byte("opaque dll")},
		{name: "Export/plugin.node", body: []byte("opaque node module")},
		{name: "Export/launcher.bat", body: []byte("opaque batch file")},
	} {
		files = appendRPGMakerFixtureFile(t, service, files, file.name, file.body)
	}
	dispositions, groups, _, err := service.prepareRPGMakerProject(
		context.Background(), "DIRECTORY", files, "rpgmaker_mv",
	)
	if err != nil {
		t.Fatal(err)
	}
	if countIgnoredDispositions(dispositions) != 1 || len(groups) != 1 || groups[0].rpgProjectRoot != "." {
		t.Fatalf("dispositions=%#v groups=%#v", dispositions, groups)
	}
	want := map[string]bool{
		"Game.exe": false, "nw.dll": false, "plugin.node": false, "launcher.bat": false,
	}
	for _, source := range groups[0].sources {
		if _, exists := want[source.logicalName]; exists && source.role == "PROJECT_FILE" {
			want[source.logicalName] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("source PROJECT_FILE %q was not preserved", name)
		}
	}
}

func TestPrepareRPGMakerArchiveMaterializesNestedEntryWithoutExpandingIt(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	archiveBytes := rpgMakerMVArchiveWithMToolSidecar(t)
	metadata, err := blobs.Put(bytes.NewReader(archiveBytes))
	if err != nil {
		t.Fatal(err)
	}
	archiveFile := importSourceFile{
		id: "archive", path: "fixture.zip", blobID: "archive", sha256: metadata.SHA256, size: metadata.Size,
	}
	_, groups, archives, err := New(nil, nil).WithBlobStore(blobs).prepareRPGMakerProject(
		context.Background(), "FILES", []importSourceFile{archiveFile}, "rpgmaker_mv",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].sources) != 11 || groups[0].titleSource != "fixture.zip" ||
		len(archives) != 1 || len(archives[0].materialized) != 11 {
		t.Fatalf("groups=%#v archives=%#v", groups, archives)
	}
	for _, entry := range archives[0].entries {
		if entry.NormalizedPath == "Export/www/audio/bgm/config" {
			if entry.NestedArchive != importing.NestedArchiveSevenZip {
				t.Fatalf("sidecar classification=%#v", entry)
			}
			if _, exists := archives[0].materialized[entry.Ordinal]; !exists {
				t.Fatalf("opaque nested ordinal %d was not materialized", entry.Ordinal)
			}
			return
		}
	}
	t.Fatal("sidecar archive entry was not indexed")
}

func TestPrepareStaticBIOSDependenciesLeavesRPGMakerValidationToRuntimePacks(t *testing.T) {
	t.Parallel()
	groups := []preparedGroup{{
		validationStatus: "BLOCKED", compatibilityCode: "RPG_RUNTIME_VALIDATION_REQUIRED",
		dependencySnapshot: `{"bindings":[],"schemaVersion":1}`,
	}}
	if err := prepareStaticBIOSDependencies(
		context.Background(), nil, "retrom-runtime", "rpgmaker-2000", "rpgmaker", groups,
	); err != nil {
		t.Fatalf("prepareStaticBIOSDependencies() error = %v", err)
	}
	if groups[0].compatibilityCode != "RPG_RUNTIME_VALIDATION_REQUIRED" {
		t.Fatalf("RPG validation was overwritten: %#v", groups[0])
	}
}

func rpgMakerMVImportFixture(t *testing.T) (*Service, []importSourceFile) {
	t.Helper()
	dataDir := t.TempDir()
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string][]byte{
		"Export/Game.exe":               []byte("desktop wrapper"),
		"Export/www/.DS_Store":          []byte("noise"),
		"Export/www/data/System.json":   []byte(`{"gameTitle":"Retrom Fixture"}`),
		"Export/www/js/rpg_core.js":     []byte(`Utils.RPGMAKER_VERSION = "1.6.2";`),
		"Export/www/js/rpg_managers.js": []byte("// fixture"),
		"Export/www/js/rpg_objects.js":  []byte("// fixture"),
		"Export/www/js/rpg_scenes.js":   []byte("// fixture"),
		"Export/www/js/rpg_sprites.js":  []byte("// fixture"),
		"Export/www/js/rpg_windows.js":  []byte("// fixture"),
		"Export/www/js/plugins.js":      []byte("// fixture"),
		"Export/www/js/main.js":         []byte("// fixture"),
	}
	scripts := []string{
		"js/rpg_core.js", "js/rpg_managers.js", "js/rpg_objects.js", "js/rpg_scenes.js",
		"js/rpg_sprites.js", "js/rpg_windows.js", "js/plugins.js", "js/main.js",
	}
	html := "<!doctype html><html><head>"
	for _, script := range scripts {
		html += `<script src="` + script + `"></script>`
	}
	contents["Export/www/index.html"] = []byte(html + "</head><body></body></html>")
	files := make([]importSourceFile, 0, len(contents))
	for name, body := range contents {
		metadata, putErr := blobs.Put(bytes.NewReader(body))
		if putErr != nil {
			t.Fatal(putErr)
		}
		files = append(files, importSourceFile{
			id: name, path: name, blobID: name, sha256: metadata.SHA256, size: metadata.Size,
		})
	}
	return New(nil, nil).WithBlobStore(blobs), files
}

func rpgMakerMVArchiveWithMToolSidecar(t *testing.T) []byte {
	t.Helper()
	type archiveFile struct {
		name string
		body []byte
	}
	scripts := []string{
		"js/rpg_core.js", "js/rpg_managers.js", "js/rpg_objects.js", "js/rpg_scenes.js",
		"js/rpg_sprites.js", "js/rpg_windows.js", "js/plugins.js", "js/main.js",
	}
	files := make([]archiveFile, 0, 3+len(scripts)+1)
	files = append(files,
		archiveFile{name: "Export/www/data/System.json", body: []byte(`{"gameTitle":"Retrom Fixture"}`)},
		archiveFile{name: "Export/www/audio/bgm/config", body: []byte("7z\xbc\xaf\x27\x1c encrypted MTool sidecar")},
		archiveFile{name: "Export/www/.DS_Store", body: []byte("packaging noise")},
	)
	html := "<!doctype html><html><head>"
	for _, script := range scripts {
		body := []byte("// fixture")
		if script == "js/rpg_core.js" {
			body = []byte(`Utils.RPGMAKER_VERSION = "1.6.2";`)
		}
		files = append(files, archiveFile{name: "Export/www/" + script, body: body})
		html += `<script src="` + script + `"></script>`
	}
	files = append(files, archiveFile{
		name: "Export/www/index.html", body: []byte(html + "</head><body></body></html>"),
	})
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, file := range files {
		entry, err := writer.Create(file.name)
		if err == nil {
			_, err = entry.Write(file.body)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func appendRPGMakerFixtureFile(
	t *testing.T,
	service *Service,
	files []importSourceFile,
	name string,
	body []byte,
) []importSourceFile {
	t.Helper()
	metadata, err := service.blobs.Put(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return append(files, importSourceFile{
		id: name, path: name, blobID: name, sha256: metadata.SHA256, size: metadata.Size,
	})
}
