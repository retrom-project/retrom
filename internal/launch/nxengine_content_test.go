package launch

import (
	"encoding/json"
	"testing"

	"retrom/internal/nxengine/detector"
)

func TestBuildNXEngineProjectIndexRequiresExecutableAndBuildsContentURLs(t *testing.T) {
	t.Parallel()
	view, err := buildNXEngineProjectIndex(
		"/runtime/projects/"+testProjectDigest+"/",
		detector.Profile{MarkerPath: "Doukutsu.exe", Compatibility: "NXENGINE_RUNTIME_TRIAL_REQUIRED"},
		[]nxengineProjectIndexFile{{Path: "Doukutsu.exe", SizeBytes: 16}, {Path: "assets/menu.png", SizeBytes: 4}},
	)
	if err != nil || len(view.Contents) == 0 || len(view.SHA256) != 64 {
		t.Fatalf("view=%#v error=%v", view, err)
	}
	var index nxengineProjectIndex
	if json.Unmarshal(view.Contents, &index) != nil || index.SchemaVersion != 1 ||
		index.Files[0].URL != "/runtime/projects/"+testProjectDigest+"/Doukutsu.exe" {
		t.Fatalf("index=%s", view.Contents)
	}
	for _, files := range [][]nxengineProjectIndexFile{
		{{Path: "assets/menu.png", SizeBytes: 4}},
		{{Path: "Doukutsu.exe", SizeBytes: 0}},
		{{Path: "Doukutsu.exe", SizeBytes: 16}, {Path: "DOUKUTSU.EXE", SizeBytes: 16}},
	} {
		if _, err := buildNXEngineProjectIndex(
			"/runtime/projects/"+testProjectDigest+"/",
			detector.Profile{MarkerPath: "Doukutsu.exe", Compatibility: "NXENGINE_RUNTIME_TRIAL_REQUIRED"}, files,
		); err == nil {
			t.Fatalf("accepted invalid files=%#v", files)
		}
	}
}
