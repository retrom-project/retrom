package launch

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	model "retrom/internal/model/launch"
)

func TestBuildKiriKiriProjectIndexPublishesEveryProjectFile(t *testing.T) {
	t.Parallel()
	entry := "data.xp3"
	root := "/runtime/content/project/" + strings.Repeat("d", 64) + "/"
	view, err := buildProjectIndexDocument(root, "", projectIndexPolicy{
		minimum: 1, maximum: 10_000, allowEmpty: true,
		marker: entry,
	}, []runtimeProjectIndexFile{
		{Path: "data.xp3", SizeBytes: 10},
		{Path: "scenario/first.ks", SizeBytes: 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	var index runtimeProjectIndex
	if err := json.Unmarshal(view.Contents, &index); err != nil {
		t.Fatal(err)
	}
	if index.SchemaVersion != 1 || len(index.Files) != 2 ||
		index.Files[0].URL != root+"data.xp3" ||
		index.Files[1].URL != root+"scenario/first.ks" || view.SHA256 == "" {
		t.Fatalf("project index = %#v, digest=%q", index, view.SHA256)
	}
}

func TestBuildKiriKiriProjectIndexRejectsUnsafeOrIncompleteFiles(t *testing.T) {
	t.Parallel()
	profile := projectIndexPolicy{minimum: 1, maximum: 10_000, allowEmpty: true, marker: "startup.tjs"}
	cases := [][]runtimeProjectIndexFile{
		{{Path: "scenario/first.ks", SizeBytes: 1}},
		{{Path: "startup.tjs", SizeBytes: -1}},
		{{Path: "startup.tjs", SizeBytes: 1}, {Path: "STARTUP.TJS", SizeBytes: 1}},
		{{Path: "startup.tjs", SizeBytes: 1}, {Path: "../escape", SizeBytes: 1}},
	}
	for _, files := range cases {
		if _, err := buildProjectIndexDocument(
			"/runtime/content/project/"+strings.Repeat("d", 64)+"/", "", profile, files,
		); !errors.Is(err, model.ErrCredential) {
			t.Fatalf("build project index error = %v for %#v", err, files)
		}
	}
}
