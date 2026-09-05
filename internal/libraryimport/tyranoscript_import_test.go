package libraryimport

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"retrom/internal/blobstore"
	"retrom/internal/importing"
)

func TestPrepareTyranoScriptDirectoryNormalizesWrapperAndRequiresRuntimeTrial(t *testing.T) {
	t.Parallel()
	service, files := tyranoScriptImportFixture(t)
	dispositions, groups, archives, err := service.prepareTyranoScriptProject(
		context.Background(), "DIRECTORY", files,
	)
	if err != nil {
		t.Fatalf("prepareTyranoScriptProject() error=%v", err)
	}
	if len(groups) != 1 || len(archives) != 0 {
		t.Fatalf("groups=%#v archives=%#v dispositions=%#v", groups, archives, dispositions)
	}
	group := groups[0]
	if group.contentKind != "TYRANOSCRIPT_PROJECT" || group.validationStatus != "BLOCKED" ||
		group.compatibilityCode != "TYRANOSCRIPT_RUNTIME_TRIAL_REQUIRED" || group.titleSource != "Fixture" ||
		countIgnoredDispositions(dispositions) != 1 {
		t.Fatalf("group=%#v dispositions=%#v", group, dispositions)
	}
	assertTyranoScriptSnapshot(t, group.dependencySnapshot)
	for _, source := range group.sources {
		if source.role != "PROJECT_FILE" || strings.HasPrefix(source.logicalName, "Fixture/") {
			t.Fatalf("TyranoScript source=%#v", source)
		}
	}
}

func TestPrepareTyranoScriptDirectoryRejectsMissingEngineMarker(t *testing.T) {
	t.Parallel()
	service, files := tyranoScriptImportFixture(t)
	filtered := files[:0]
	for _, file := range files {
		if !strings.HasSuffix(file.path, "tyrano/tyrano.js") {
			filtered = append(filtered, file)
		}
	}
	if _, _, _, err := service.prepareTyranoScriptProject(context.Background(), "DIRECTORY", filtered); err == nil ||
		!strings.Contains(err.Error(), "TYRANOSCRIPT_PROJECT_INVALID") {
		t.Fatalf("missing engine marker error=%v", err)
	}
}

func TestPrepareTyranoScriptNWJSExecutableExtractsAppendedProject(t *testing.T) {
	t.Parallel()
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	executable := tyranoScriptNWJSExecutable(t)
	metadata, err := blobs.Put(bytes.NewReader(executable))
	if err != nil {
		t.Fatal(err)
	}
	file := importSourceFile{
		id: "game", path: "game.exe", blobID: "game", sha256: metadata.SHA256, size: metadata.Size,
	}
	dispositions, groups, archives, err := New(nil, nil).WithBlobStore(blobs).prepareTyranoScriptProject(
		context.Background(), "FILES", []importSourceFile{file},
	)
	if err != nil {
		t.Fatalf("prepareTyranoScriptProject(NW.js executable) error=%v", err)
	}
	if len(dispositions) != 1 || dispositions[0].disposition != "SOURCE" ||
		len(groups) != 1 || len(archives) != 1 || len(groups[0].sources) != 5 {
		t.Fatalf("dispositions=%#v groups=%#v archives=%#v", dispositions, groups, archives)
	}
	assertTyranoScriptSnapshot(t, groups[0].dependencySnapshot)
	for _, source := range groups[0].sources {
		if source.role != "PROJECT_FILE" || source.archiveOrdinal == nil {
			t.Fatalf("TyranoScript executable source=%#v", source)
		}
	}
}

func TestPrepareTyranoScriptWrappedNWJSArchiveExtractsAppendedProject(t *testing.T) {
	t.Parallel()
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	archiveBytes := tyranoScriptWrappedNWJSArchive(t)
	metadata, err := blobs.Put(bytes.NewReader(archiveBytes))
	if err != nil {
		t.Fatal(err)
	}
	file := importSourceFile{
		id: "wrapped-nwjs", path: "wrapped-nwjs.zip", blobID: "wrapped-nwjs",
		sha256: metadata.SHA256, size: metadata.Size,
	}
	dispositions, groups, archives, err := New(nil, nil).WithBlobStore(blobs).prepareTyranoScriptProject(
		context.Background(), "FILES", []importSourceFile{file},
	)
	if err != nil {
		t.Fatalf("prepareTyranoScriptProject(wrapped NW.js executable) error=%v", err)
	}
	if len(dispositions) != 1 || dispositions[0].disposition != "SOURCE" ||
		len(groups) != 1 || len(archives) != 1 || len(groups[0].sources) != 5 ||
		len(archives[0].entries) != 5 {
		t.Fatalf("dispositions=%#v groups=%#v archives=%#v", dispositions, groups, archives)
	}
	for _, entry := range archives[0].entries {
		if strings.HasPrefix(entry.NormalizedPath, "Desktop/") || strings.HasSuffix(entry.NormalizedPath, ".exe") {
			t.Fatalf("desktop wrapper leaked into project entries: %#v", entry)
		}
	}
	assertTyranoScriptSnapshot(t, groups[0].dependencySnapshot)
}

func TestPrepareTyranoScriptWrappedNWJSArchiveRejectsMultipleExecutables(t *testing.T) {
	t.Parallel()
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for _, name := range []string{"Desktop/game.exe", "Desktop/other.exe"} {
		writer, createErr := archive.Create(name)
		if createErr == nil {
			_, createErr = writer.Write(tyranoScriptNWJSExecutable(t))
		}
		if createErr != nil {
			t.Fatal(createErr)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	metadata, err := blobs.Put(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	file := importSourceFile{
		id: "ambiguous", path: "ambiguous.zip", blobID: "ambiguous",
		sha256: metadata.SHA256, size: metadata.Size,
	}
	_, _, _, err = New(nil, nil).WithBlobStore(blobs).prepareTyranoScriptProject(
		context.Background(), "FILES", []importSourceFile{file},
	)
	if !errors.Is(err, importing.ErrArchiveUnsafe) {
		t.Fatalf("multiple wrapped NW.js executables error=%v", err)
	}
}

func TestPrepareTyranoScriptElectronArchiveExtractsASARProject(t *testing.T) {
	t.Parallel()
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	archiveBytes := tyranoScriptElectronArchive(t)
	metadata, err := blobs.Put(bytes.NewReader(archiveBytes))
	if err != nil {
		t.Fatal(err)
	}
	file := importSourceFile{
		id: "electron", path: "electron.zip", blobID: "electron", sha256: metadata.SHA256, size: metadata.Size,
	}
	dispositions, groups, archives, err := New(nil, nil).WithBlobStore(blobs).prepareTyranoScriptProject(
		context.Background(), "FILES", []importSourceFile{file},
	)
	if err != nil {
		t.Fatalf("prepareTyranoScriptProject(Electron) error=%v", err)
	}
	if len(dispositions) != 1 || dispositions[0].disposition != "SOURCE" ||
		len(groups) != 1 || len(archives) != 1 || len(groups[0].sources) != 5 {
		t.Fatalf("dispositions=%#v groups=%#v archives=%#v", dispositions, groups, archives)
	}
	for _, entry := range archives[0].entries {
		if entry.ArchiveFormat != "ELECTRON_ASAR" || entry.CompressionProfile != "ELECTRON_ASAR_DEFLATE" {
			t.Fatalf("Electron archive entry=%#v", entry)
		}
	}
	assertTyranoScriptSnapshot(t, groups[0].dependencySnapshot)
}

func assertTyranoScriptSnapshot(t *testing.T, contents string) {
	t.Helper()
	var snapshot struct {
		SchemaVersion int `json:"schemaVersion"`
		TyranoScript  struct {
			EntryPath     string `json:"entryPath"`
			Compatibility string `json:"compatibility"`
		} `json:"tyranoScript"`
	}
	if json.Unmarshal([]byte(contents), &snapshot) != nil || snapshot.SchemaVersion != 1 ||
		snapshot.TyranoScript.EntryPath != "index.html" ||
		snapshot.TyranoScript.Compatibility != "TYRANOSCRIPT_RUNTIME_TRIAL_REQUIRED" {
		t.Fatalf("dependency snapshot=%s", contents)
	}
}

func tyranoScriptImportFixture(t *testing.T) (*Service, []importSourceFile) {
	t.Helper()
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string][]byte{
		"Fixture/index.html":                      []byte("<!doctype html><title>Fixture</title>"),
		"Fixture/data/scenario/first.ks":          []byte("*start\n[text val='fixture']"),
		"Fixture/data/system/Config.tjs":          []byte(";config"),
		"Fixture/tyrano/plugins/kag/kag.js":       []byte("window.tyrano={};"),
		"Fixture/tyrano/tyrano.js":                []byte("window.TYRANO={};"),
		"Fixture/data/image/title/background.png": []byte("fixture-image"),
		"Fixture/.DS_Store":                       []byte("noise"),
	}
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

func tyranoScriptNWJSExecutable(t *testing.T) []byte {
	t.Helper()
	pe := make([]byte, 512)
	copy(pe, "MZ")
	pe[0x3c] = 0x80
	copy(pe[0x80:], "PE\x00\x00")
	pe[0x84], pe[0x85] = 0x4c, 0x01
	pe[0x86] = 0x01
	var output bytes.Buffer
	_, _ = output.Write(pe)
	archive := zip.NewWriter(&output)
	files := map[string]string{
		"index.html":                "<!doctype html><html><head></head><body></body></html>",
		"data/scenario/first.ks":    "*start\n[cm]",
		"data/system/Config.tjs":    ";configSave=file",
		"tyrano/plugins/kag/kag.js": "window.tyrano = window.tyrano || {};",
		"tyrano/tyrano.js":          "window.TYRANO = window.TYRANO || {};",
	}
	for name, contents := range files {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func tyranoScriptWrappedNWJSArchive(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for name, contents := range map[string][]byte{
		"Desktop/Sera's Adventure.exe": tyranoScriptNWJSExecutable(t),
		"Desktop/d3dcompiler_47.dll":   []byte("desktop runtime sidecar"),
	} {
		writer, err := archive.Create(name)
		if err == nil {
			_, err = writer.Write(contents)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func tyranoScriptElectronArchive(t *testing.T) []byte {
	t.Helper()
	files := map[string][]byte{
		"index.html":                []byte("<!doctype html><html><head></head><body></body></html>"),
		"data/scenario/first.ks":    []byte("*start\n[cm]"),
		"data/system/Config.tjs":    []byte(";configSave=file"),
		"tyrano/plugins/kag/kag.js": []byte("window.tyrano = window.tyrano || {};"),
		"tyrano/tyrano.js":          []byte("window.TYRANO = window.TYRANO || {};"),
	}
	paths := make([]string, 0, len(files))
	for name := range files {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	header := map[string]any{"files": map[string]any{}}
	var body bytes.Buffer
	for _, name := range paths {
		insertTyranoScriptASARNode(t, header, name, map[string]any{
			"size": len(files[name]), "offset": fmt.Sprintf("%d", body.Len()),
		})
		_, _ = body.Write(files[name])
	}
	jsonHeader, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	payloadSize := 4 + len(jsonHeader)
	payloadSize += (4 - payloadSize%4) % 4
	appASAR := make([]byte, 12+payloadSize+body.Len())
	binary.LittleEndian.PutUint32(appASAR[0:4], 4)
	binary.LittleEndian.PutUint32(appASAR[4:8], uint32(4+payloadSize))
	binary.LittleEndian.PutUint32(appASAR[8:12], uint32(payloadSize))
	binary.LittleEndian.PutUint32(appASAR[12:16], uint32(len(jsonHeader)))
	copy(appASAR[16:], jsonHeader)
	copy(appASAR[12+payloadSize:], body.Bytes())
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for name, contents := range map[string][]byte{
		"Fixture/Fixture.exe":        []byte("MZ synthetic Electron shell"),
		"Fixture/resources/app.asar": appASAR,
	} {
		writer, createErr := archive.Create(name)
		if createErr == nil {
			_, createErr = writer.Write(contents)
		}
		if createErr != nil {
			t.Fatal(createErr)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func insertTyranoScriptASARNode(t *testing.T, root map[string]any, name string, value map[string]any) {
	t.Helper()
	node := root
	parts := strings.Split(name, "/")
	for _, part := range parts[:len(parts)-1] {
		files, ok := node["files"].(map[string]any)
		if !ok {
			t.Fatal("invalid TyranoScript ASAR test directory")
		}
		child, exists := files[part]
		if !exists {
			child = map[string]any{"files": map[string]any{}}
			files[part] = child
		}
		node, ok = child.(map[string]any)
		if !ok {
			t.Fatal("TyranoScript ASAR test path conflicts with a file")
		}
	}
	files, ok := node["files"].(map[string]any)
	if !ok {
		t.Fatal("invalid TyranoScript ASAR test leaf directory")
	}
	files[parts[len(parts)-1]] = value
}
