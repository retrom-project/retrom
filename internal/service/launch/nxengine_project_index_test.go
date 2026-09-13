package launch

import (
	"encoding/json"
	"testing"
)

func TestBuildNXEngineProjectIndexRequiresExecutableAndBuildsContentURLs(t *testing.T) {
	t.Parallel()
	view, err := buildProjectIndexDocument(
		"/runtime/projects/"+testProjectDigest+"/", "",
		projectIndexPolicy{minimum: 1, maximum: 4096, marker: "Doukutsu.exe"},
		[]runtimeProjectIndexFile{{Path: "Doukutsu.exe", SizeBytes: 16}, {Path: "assets/menu.png", SizeBytes: 4}},
	)
	if err != nil || len(view.Contents) == 0 || len(view.SHA256) != 64 {
		t.Fatalf("view=%#v error=%v", view, err)
	}
	var index runtimeProjectIndex
	if json.Unmarshal(view.Contents, &index) != nil || index.SchemaVersion != 1 ||
		index.Files[0].URL != "/runtime/projects/"+testProjectDigest+"/Doukutsu.exe" {
		t.Fatalf("index=%s", view.Contents)
	}
	for _, files := range [][]runtimeProjectIndexFile{
		{{Path: "assets/menu.png", SizeBytes: 4}},
		{{Path: "Doukutsu.exe", SizeBytes: 0}},
		{{Path: "Doukutsu.exe", SizeBytes: 16}, {Path: "DOUKUTSU.EXE", SizeBytes: 16}},
	} {
		if _, err := buildProjectIndexDocument(
			"/runtime/projects/"+testProjectDigest+"/", "",
			projectIndexPolicy{minimum: 1, maximum: 4096, marker: "Doukutsu.exe"}, files,
		); err == nil {
			t.Fatalf("accepted invalid files=%#v", files)
		}
	}
}
