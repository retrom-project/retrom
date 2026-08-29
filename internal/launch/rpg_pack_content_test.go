package launch

import (
	"errors"
	"testing"
)

func TestBuildRuntimePackIndexUsesPerFileRuntimeURLs(t *testing.T) {
	t.Parallel()
	view, err := buildRuntimePackIndex("01980000-0000-7000-8000-000000000001", 1, []runtimePackFileIndexEntry{
		{Path: "CharSet/Hero.png", SizeBytes: 41},
		{Path: "Music/Theme #1.ogg", SizeBytes: 42},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"files":[{"path":"CharSet/Hero.png","sizeBytes":41,"url":"/runtime/projects/01980000-0000-7000-8000-000000000001/__retrom__/packs/1/files/CharSet/Hero.png"},{"path":"Music/Theme #1.ogg","sizeBytes":42,"url":"/runtime/projects/01980000-0000-7000-8000-000000000001/__retrom__/packs/1/files/Music/Theme%20%231.ogg"}],"schemaVersion":1}`
	if string(view.Contents) != want || len(view.SHA256) != 64 {
		t.Fatalf("runtime pack index = %s / %s", view.Contents, view.SHA256)
	}
}

func TestBuildRuntimePackIndexRejectsAmbiguousOrInvalidEntries(t *testing.T) {
	t.Parallel()
	for name, entries := range map[string][]runtimePackFileIndexEntry{
		"case collision": {{Path: "Music/Theme.ogg", SizeBytes: 1}, {Path: "music/theme.ogg", SizeBytes: 1}},
		"empty file":     {{Path: "Music/Theme.ogg", SizeBytes: 0}},
		"traversal":      {{Path: "../Theme.ogg", SizeBytes: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := buildRuntimePackIndex("launch", 0, entries); !errors.Is(err, ErrCredential) {
				t.Fatalf("error = %v, want %v", err, ErrCredential)
			}
		})
	}
}
