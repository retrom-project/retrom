package launch

import (
	"encoding/json"
	"testing"
)

const testProjectDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestBuildButterscotchProjectIndexRequiresDataWinAndBuildsContentURLs(t *testing.T) {
	t.Parallel()
	view, err := buildProjectIndexDocument(
		"/runtime/projects/"+testProjectDigest+"/", "",
		projectIndexPolicy{minimum: 1, maximum: 10_000, marker: "data.win"},
		[]runtimeProjectIndexFile{{Path: "data.win", SizeBytes: 16}, {Path: "assets/menu.png", SizeBytes: 4}},
	)
	if err != nil || len(view.Contents) == 0 || len(view.SHA256) != 64 {
		t.Fatalf("view=%#v error=%v", view, err)
	}
	var index runtimeProjectIndex
	if json.Unmarshal(view.Contents, &index) != nil || index.SchemaVersion != 1 ||
		index.Files[0].URL != "/runtime/projects/"+testProjectDigest+"/data.win" {
		t.Fatalf("index=%s", view.Contents)
	}
	for _, files := range [][]runtimeProjectIndexFile{
		{{Path: "assets/menu.png", SizeBytes: 4}},
		{{Path: "data.win", SizeBytes: 0}},
		{{Path: "data.win", SizeBytes: 16}, {Path: "DATA.WIN", SizeBytes: 16}},
	} {
		if _, err := buildProjectIndexDocument(
			"/runtime/projects/"+testProjectDigest+"/", "",
			projectIndexPolicy{minimum: 1, maximum: 10_000, marker: "data.win"}, files,
		); err == nil {
			t.Fatalf("accepted invalid files=%#v", files)
		}
	}
}
