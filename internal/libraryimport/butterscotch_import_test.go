package libraryimport

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"retrom/internal/blobstore"
	"retrom/internal/importing"
	"retrom/internal/rpgmaker/fileset"
)

func TestPrepareButterscotchDirectoryNormalizesWrapperAndRequiresRuntimeTrial(t *testing.T) {
	t.Parallel()
	service, files := butterscotchImportFixture(t)
	dispositions, groups, archives, err := service.prepareButterscotchProject(
		context.Background(), "DIRECTORY", files,
	)
	if err != nil {
		t.Fatalf("prepareButterscotchProject() error=%v", err)
	}
	if !validButterscotchPreparedResult(dispositions, groups, archives) {
		t.Fatalf("groups=%#v archives=%#v dispositions=%#v", groups, archives, dispositions)
	}
	assertButterscotchSnapshot(t, groups[0].dependencySnapshot)
	assertButterscotchSources(t, groups[0].sources)
}

func validButterscotchPreparedResult(
	dispositions []preparedDisposition,
	groups []preparedGroup,
	archives []preparedArchive,
) bool {
	if len(groups) != 1 || len(archives) != 0 {
		return false
	}
	group := groups[0]
	return group.contentKind == "BUTTERSCOTCH_PROJECT_V1" && group.validationStatus == "BLOCKED" &&
		group.compatibilityCode == "BUTTERSCOTCH_RUNTIME_TRIAL_REQUIRED" &&
		group.titleSource == "Fixture" && countIgnoredDispositions(dispositions) == 1
}

func assertButterscotchSnapshot(t *testing.T, dependencySnapshot string) {
	t.Helper()
	var snapshot struct {
		SchemaVersion int `json:"schemaVersion"`
		Butterscotch  struct {
			MarkerPath    string `json:"markerPath"`
			Compatibility string `json:"compatibility"`
		} `json:"butterscotch"`
	}
	if json.Unmarshal([]byte(dependencySnapshot), &snapshot) != nil ||
		snapshot.SchemaVersion != 1 || snapshot.Butterscotch.MarkerPath != "data.win" ||
		snapshot.Butterscotch.Compatibility != "GAMEMAKER_RUNTIME_TRIAL_REQUIRED" {
		t.Fatalf("dependency snapshot=%s", dependencySnapshot)
	}
}

func assertButterscotchSources(t *testing.T, sources []preparedSource) {
	t.Helper()
	for _, source := range sources {
		if source.role != "PROJECT_FILE" || strings.HasPrefix(source.logicalName, "Fixture/") {
			t.Fatalf("Butterscotch source=%#v", source)
		}
	}
}

func TestPrepareButterscotchDirectoryRejectsInvalidDataWin(t *testing.T) {
	t.Parallel()
	service, files := butterscotchImportFixture(t)
	for index := range files {
		if strings.EqualFold(files[index].path, "Fixture/data.win") {
			metadata, err := service.blobs.Put(bytes.NewReader([]byte("invalid")))
			if err != nil {
				t.Fatal(err)
			}
			files[index].sha256, files[index].size = metadata.SHA256, metadata.Size
		}
	}
	if _, _, _, err := service.prepareButterscotchProject(context.Background(), "DIRECTORY", files); err == nil ||
		!strings.Contains(err.Error(), "BUTTERSCOTCH_PROJECT_INVALID") {
		t.Fatalf("invalid data.win error=%v", err)
	}
}

func TestArchiveProjectPathsRejectsMissingMaterializedEntry(t *testing.T) {
	t.Parallel()
	files := []fileset.SourceFile{{Path: "data.win", SourceIndex: 7}}
	if _, err := archiveProjectPaths(files, map[int]blobstore.Metadata{}); !errors.Is(err, importing.ErrArchiveUnsafe) {
		t.Fatalf("archiveProjectPaths() error=%v, want ARCHIVE_UNSAFE", err)
	}
}

func butterscotchImportFixture(t *testing.T) (*Service, []importSourceFile) {
	t.Helper()
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dataWin := make([]byte, 16)
	copy(dataWin, "FORM")
	binary.LittleEndian.PutUint32(dataWin[4:], 8)
	copy(dataWin[8:], "GEN8")
	contents := map[string][]byte{
		"Fixture/data.win":    dataWin,
		"Fixture/options.ini": []byte("[options]"),
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
