package detector

import (
	"strings"
	"testing"
)

type memoryIndex struct{ files []File }

func (index memoryIndex) Files() []File { return append([]File(nil), index.files...) }

func TestDetectLooseKAGProject(t *testing.T) {
	t.Parallel()
	profile, err := Detect(memoryIndex{files: []File{
		{Path: "startup.tjs", Size: 20},
		{Path: "system/MainWindow.tjs", Size: 100},
		{Path: "scenario/first.ks", Size: 100},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if profile.MarkerPath != "startup.tjs" || profile.StartupXP3Path != nil ||
		profile.Compatibility != "KAG_RUNTIME_TRIAL_REQUIRED" {
		t.Fatalf("profile = %#v", profile)
	}
	encoded, err := MarshalSnapshot(profile)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseSnapshot(string(encoded))
	if err != nil || decoded.MarkerPath != profile.MarkerPath {
		t.Fatalf("ParseSnapshot() = %#v, %v", decoded, err)
	}
}

func TestDetectSelectsDataXP3AndRejectsAmbiguousAlternatives(t *testing.T) {
	t.Parallel()
	profile, err := Detect(memoryIndex{files: []File{
		{Path: "startup.tjs", Size: 20}, {Path: "patch.xp3", Size: 40}, {Path: "data.xp3", Size: 50},
	}})
	if err != nil || profile.StartupXP3Path == nil || *profile.StartupXP3Path != "data.xp3" {
		t.Fatalf("Detect() = %#v, %v", profile, err)
	}
	_, err = Detect(memoryIndex{files: []File{
		{Path: "startup.tjs", Size: 20}, {Path: "one.xp3", Size: 40}, {Path: "two.xp3", Size: 50},
	}})
	if err == nil || !strings.Contains(err.Error(), "ambiguous XP3") {
		t.Fatalf("ambiguous Detect() error = %v", err)
	}
}

func TestDetectRejectsUnrelatedProject(t *testing.T) {
	t.Parallel()
	if _, err := Detect(memoryIndex{files: []File{{Path: "game.exe", Size: 1}}}); err == nil {
		t.Fatal("Detect() accepted unrelated project")
	}
}
