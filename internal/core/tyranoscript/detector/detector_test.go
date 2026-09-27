package detector

import (
	"errors"
	"testing"
)

type memoryIndex []File

func (index memoryIndex) Files() []File { return append([]File(nil), index...) }

func TestDetectAcceptsCompleteBrowserProject(t *testing.T) {
	t.Parallel()
	profile, err := Detect(memoryIndex{
		{Path: "index.html", Size: 1024},
		{Path: "data/scenario/first.ks", Size: 32},
		{Path: "data/system/Config.tjs", Size: 64},
		{Path: "tyrano/plugins/kag/kag.js", Size: 256},
		{Path: "tyrano/tyrano.js", Size: 128},
	})
	if err != nil || profile.EntryPath != "index.html" ||
		profile.Compatibility != "TYRANOSCRIPT_RUNTIME_TRIAL_REQUIRED" {
		t.Fatalf("profile=%#v error=%v", profile, err)
	}
}

func TestDetectRejectsMissingAndCaseDuplicateMarkers(t *testing.T) {
	t.Parallel()
	valid := memoryIndex{
		{Path: "index.html", Size: 1},
		{Path: "data/scenario/first.ks", Size: 1},
		{Path: "data/system/Config.tjs", Size: 1},
		{Path: "tyrano/plugins/kag/kag.js", Size: 1},
		{Path: "tyrano/tyrano.js", Size: 1},
	}
	for _, index := range []memoryIndex{valid[:4], append(valid, File{Path: "INDEX.HTML", Size: 1})} {
		if _, err := Detect(index); !errors.Is(err, ErrProjectInvalid) {
			t.Fatalf("Detect(%#v) error=%v", index, err)
		}
	}
}

func TestSnapshotRoundTripRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	profile := Profile{EntryPath: "index.html", Compatibility: "TYRANOSCRIPT_RUNTIME_TRIAL_REQUIRED"}
	contents, err := MarshalSnapshot(profile)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSnapshot(string(contents))
	if err != nil || parsed != profile {
		t.Fatalf("ParseSnapshot()=%#v, %v", parsed, err)
	}
	if _, err := ParseSnapshot(`{"schemaVersion":1,"tyranoScript":{"entryPath":"index.html","compatibility":"TYRANOSCRIPT_RUNTIME_TRIAL_REQUIRED"},"extra":true}`); !errors.Is(err, ErrProjectInvalid) {
		t.Fatalf("unknown field error=%v", err)
	}
}
