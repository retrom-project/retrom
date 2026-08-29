package libraryimport

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"retrom/internal/blobstore"
)

func TestPrepareKiriKiriDirectoryNormalizesWrapperAndRequiresRuntimeTrial(t *testing.T) {
	t.Parallel()
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := storeKiriKiriDirectoryFixture(t, blobs, map[string][]byte{
		"Fixture/startup.tjs": []byte("Scripts.execStorage('system/Initialize.tjs');"),
		"Fixture/data.xp3":    []byte("fixture XP3"),
		"Fixture/.DS_Store":   []byte("noise"),
	})
	service := New(nil, nil).WithBlobStore(blobs)
	dispositions, groups, archives, err := service.prepareKiriKiriProject(context.Background(), "DIRECTORY", files)
	if err != nil {
		t.Fatal(err)
	}
	assertKiriKiriPreparedDirectory(t, dispositions, groups, archives)
}

func storeKiriKiriDirectoryFixture(
	t *testing.T,
	blobs *blobstore.Store,
	contents map[string][]byte,
) []importSourceFile {
	t.Helper()
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
	return files
}

func assertKiriKiriPreparedDirectory(
	t *testing.T,
	dispositions []preparedDisposition,
	groups []preparedGroup,
	archives []preparedArchive,
) {
	t.Helper()
	if len(groups) != 1 || len(archives) != 0 {
		t.Fatalf("groups=%#v archives=%#v dispositions=%#v", groups, archives, dispositions)
	}
	group := groups[0]
	if group.contentKind != "KIRIKIRI_PROJECT_V1" || group.validationStatus != "BLOCKED" {
		t.Fatalf("group=%#v", group)
	}
	if group.compatibilityCode != "KIRIKIRI_RUNTIME_TRIAL_REQUIRED" || len(group.sources) != 2 {
		t.Fatalf("group=%#v", group)
	}
	if countIgnoredDispositions(dispositions) != 1 {
		t.Fatalf("dispositions=%#v", dispositions)
	}
	var snapshot struct {
		SchemaVersion int `json:"schemaVersion"`
		KiriKiri      struct {
			MarkerPath     string  `json:"markerPath"`
			StartupXP3Path *string `json:"startupXp3Path"`
			Compatibility  string  `json:"compatibility"`
		} `json:"kirikiri"`
	}
	if json.Unmarshal([]byte(group.dependencySnapshot), &snapshot) != nil || snapshot.SchemaVersion != 1 {
		t.Fatalf("dependency snapshot = %s", group.dependencySnapshot)
	}
	if snapshot.KiriKiri.MarkerPath != "startup.tjs" || snapshot.KiriKiri.StartupXP3Path == nil {
		t.Fatalf("dependency snapshot = %s", group.dependencySnapshot)
	}
	if *snapshot.KiriKiri.StartupXP3Path != "data.xp3" ||
		snapshot.KiriKiri.Compatibility != "KAG_RUNTIME_TRIAL_REQUIRED" {
		t.Fatalf("dependency snapshot = %s", group.dependencySnapshot)
	}
}
