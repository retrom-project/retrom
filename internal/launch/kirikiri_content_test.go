package launch

import (
	"encoding/json"
	"errors"
	"testing"

	"retrom/internal/kirikiri/detector"
)

func TestBuildKiriKiriProjectIndexPublishesEveryProjectFile(t *testing.T) {
	t.Parallel()
	entry := "data.xp3"
	view, err := buildKiriKiriProjectIndex("launch-id", detector.Profile{
		MarkerPath: entry, StartupXP3Path: &entry, Compatibility: "KAG_RUNTIME_TRIAL_REQUIRED",
	}, []kirikiriProjectIndexFile{
		{Path: "data.xp3", SizeBytes: 10},
		{Path: "scenario/first.ks", SizeBytes: 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	var index kirikiriProjectIndex
	if err := json.Unmarshal(view.Contents, &index); err != nil {
		t.Fatal(err)
	}
	if index.SchemaVersion != 1 || len(index.Files) != 2 ||
		index.Files[0].URL != "/runtime/projects/launch-id/data.xp3" ||
		index.Files[1].URL != "/runtime/projects/launch-id/scenario/first.ks" || view.SHA256 == "" {
		t.Fatalf("project index = %#v, digest=%q", index, view.SHA256)
	}
}

func TestBuildKiriKiriProjectIndexRejectsUnsafeOrIncompleteFiles(t *testing.T) {
	t.Parallel()
	profile := detector.Profile{MarkerPath: "startup.tjs", Compatibility: "KAG_RUNTIME_TRIAL_REQUIRED"}
	cases := [][]kirikiriProjectIndexFile{
		{{Path: "scenario/first.ks", SizeBytes: 1}},
		{{Path: "startup.tjs", SizeBytes: -1}},
		{{Path: "startup.tjs", SizeBytes: 1}, {Path: "STARTUP.TJS", SizeBytes: 1}},
		{{Path: "startup.tjs", SizeBytes: 1}, {Path: "../escape", SizeBytes: 1}},
	}
	for _, files := range cases {
		if _, err := buildKiriKiriProjectIndex("launch-id", profile, files); !errors.Is(err, ErrCredential) {
			t.Fatalf("build project index error = %v for %#v", err, files)
		}
	}
}
