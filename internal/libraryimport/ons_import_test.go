package libraryimport

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"retrom/internal/blobstore"
)

func TestPrepareONSDirectoryNormalizesWrapperAndRequiresRuntimeTrial(t *testing.T) {
	t.Parallel()
	service, files := onsImportFixture(t)
	dispositions, groups, archives, err := service.prepareONSProject(
		context.Background(), "DIRECTORY", files,
	)
	if err != nil {
		t.Fatalf("prepareONSProject() error = %v", err)
	}
	if len(groups) != 1 || len(archives) != 0 {
		t.Fatalf("groups=%#v archives=%#v dispositions=%#v", groups, archives, dispositions)
	}
	assertONSPreparedGroup(t, groups[0], dispositions)
	var snapshot struct {
		SchemaVersion int `json:"schemaVersion"`
		ONS           struct {
			FontPath       string `json:"fontPath"`
			MarkerPath     string `json:"markerPath"`
			ScriptEncoding string `json:"scriptEncoding"`
		} `json:"ons"`
	}
	if json.Unmarshal([]byte(groups[0].dependencySnapshot), &snapshot) != nil || snapshot.SchemaVersion != 1 ||
		snapshot.ONS.FontPath != "default.ttf" || snapshot.ONS.MarkerPath != "0.txt" ||
		snapshot.ONS.ScriptEncoding != "utf8" {
		t.Fatalf("dependency snapshot = %s", groups[0].dependencySnapshot)
	}
	for _, source := range groups[0].sources {
		if source.role != "PROJECT_FILE" || strings.HasPrefix(source.logicalName, "Fixture/") {
			t.Fatalf("ONS source = %#v", source)
		}
	}
}

func assertONSPreparedGroup(t *testing.T, group preparedGroup, dispositions []preparedDisposition) {
	t.Helper()
	if group.contentKind != "ONS_PROJECT" || group.validationStatus != "BLOCKED" ||
		group.compatibilityCode != "ONS_RUNTIME_TRIAL_REQUIRED" || group.titleSource != "Fixture" ||
		len(group.sources) != 3 || countIgnoredDispositions(dispositions) != 1 {
		t.Fatalf("group=%#v dispositions=%#v", group, dispositions)
	}
}

func TestPrepareONSDirectoryRejectsProjectWithoutFont(t *testing.T) {
	t.Parallel()
	service, files := onsImportFixture(t)
	withoutFont := make([]importSourceFile, 0, len(files))
	for _, file := range files {
		if !strings.HasSuffix(file.path, ".ttf") {
			withoutFont = append(withoutFont, file)
		}
	}
	if _, _, _, err := service.prepareONSProject(context.Background(), "DIRECTORY", withoutFont); err == nil ||
		!strings.Contains(err.Error(), "ONS_PROJECT_INVALID") {
		t.Fatalf("missing font error = %v", err)
	}
}

func onsImportFixture(t *testing.T) (*Service, []importSourceFile) {
	t.Helper()
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string][]byte{
		"Fixture/0.txt":       []byte("*define\n*start\n"),
		"Fixture/default.ttf": []byte("fixture-font"),
		"Fixture/arc.nsa":     []byte("opaque ONS archive"),
		"Fixture/.DS_Store":   []byte("noise"),
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
