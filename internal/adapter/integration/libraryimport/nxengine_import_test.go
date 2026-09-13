package libraryimport

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"retrom/internal/adapter/files/blobstore"
)

func TestPrepareNXEngineDirectoryNormalizesWrapperAndRequiresRuntimeTrial(t *testing.T) {
	t.Parallel()
	service, files := nxengineImportFixture(t)
	dispositions, groups, archives, err := service.prepareNXEngineProject(
		context.Background(), "DIRECTORY", files,
	)
	if err != nil {
		t.Fatalf("prepareNXEngineProject() error=%v", err)
	}
	if !validNXEnginePreparedResult(dispositions, groups, archives) {
		t.Fatalf("groups=%#v archives=%#v dispositions=%#v", groups, archives, dispositions)
	}
	assertNXEngineSnapshot(t, groups[0].DependencySnapshot)
	assertNXEngineSources(t, groups[0].Sources)
}

func validNXEnginePreparedResult(
	dispositions []preparedDisposition,
	groups []preparedGroup,
	archives []preparedArchive,
) bool {
	if len(groups) != 1 || len(archives) != 0 {
		return false
	}
	group := groups[0]
	return group.ContentKind == "NXENGINE_PROJECT" && group.ValidationStatus == "BLOCKED" &&
		group.CompatibilityCode == "NXENGINE_RUNTIME_TRIAL_REQUIRED" &&
		group.TitleSource == "Fixture" && countIgnoredDispositions(dispositions) == 1
}

func assertNXEngineSnapshot(t *testing.T, dependencySnapshot string) {
	t.Helper()
	var snapshot struct {
		SchemaVersion int `json:"schemaVersion"`
		NXEngine      struct {
			MarkerPath    string `json:"markerPath"`
			Compatibility string `json:"compatibility"`
		} `json:"nxengine"`
	}
	if json.Unmarshal([]byte(dependencySnapshot), &snapshot) != nil ||
		snapshot.SchemaVersion != 1 || snapshot.NXEngine.MarkerPath != "Doukutsu.exe" ||
		snapshot.NXEngine.Compatibility != "NXENGINE_RUNTIME_TRIAL_REQUIRED" {
		t.Fatalf("dependency snapshot=%s", dependencySnapshot)
	}
}

func assertNXEngineSources(t *testing.T, sources []preparedSource) {
	t.Helper()
	for _, source := range sources {
		if source.Role != "PROJECT_FILE" || strings.HasPrefix(source.LogicalName, "Fixture/") {
			t.Fatalf("NXEngine source=%#v", source)
		}
	}
}

func TestPrepareNXEngineDirectoryRejectsInvalidExecutable(t *testing.T) {
	t.Parallel()
	service, files := nxengineImportFixture(t)
	for index := range files {
		if strings.EqualFold(files[index].Path, "Fixture/Doukutsu.exe") {
			metadata, err := service.blobs.Put(bytes.NewReader([]byte("invalid")))
			if err != nil {
				t.Fatal(err)
			}
			files[index].SHA256, files[index].Size = metadata.SHA256, metadata.Size
		}
	}
	if _, _, _, err := service.prepareNXEngineProject(context.Background(), "DIRECTORY", files); err == nil ||
		!strings.Contains(err.Error(), "NXENGINE_PROJECT_INVALID") {
		t.Fatalf("invalid Doukutsu.exe error=%v", err)
	}
}

func nxengineImportFixture(t *testing.T) (*Service, []importSourceFile) {
	t.Helper()
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string][]byte{
		"Fixture/Doukutsu.exe":         append([]byte("MZ"), make([]byte, 126)...),
		"Fixture/data/npc.tbl":         {1},
		"Fixture/data/Stage/Start.pxm": {1},
		"Fixture/data/Stage/Start.tsc": {1},
		"Fixture/.DS_Store":            []byte("noise"),
	}
	files := make([]importSourceFile, 0, len(contents))
	for name, body := range contents {
		metadata, putErr := blobs.Put(bytes.NewReader(body))
		if putErr != nil {
			t.Fatal(putErr)
		}
		files = append(files, importSourceFile{
			ID: name, Path: name, BlobID: name, SHA256: metadata.SHA256, Size: metadata.Size,
		})
	}
	return New(nil, nil).WithBlobStore(blobs), files
}
