package libraryimport

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"retrom/internal/blobstore"
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
	if group.contentKind != "TYRANOSCRIPT_PROJECT_V1" || group.validationStatus != "BLOCKED" ||
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
