package launch

import (
	"errors"
	"fmt"
	"testing"
)

func TestPreviewContentFreezesDOSAndOrderedDiscs(t *testing.T) {
	t.Parallel()
	_, repository, _, _ := previewFixture(t)
	snapshot := repository.snapshot
	snapshot.Source.ContentKind = "DOS_BUNDLE"
	snapshot.ValidationFiles = []PreviewFile{{Role: "DOS_LAUNCH_BUNDLE", LogicalName: "game.zip", BlobID: "dos-bundle"}}
	content, err := previewContent(snapshot)
	if err != nil || content.BlobID != "dos-bundle" || content.Format != "RETROM_DOS_DIRECT_ZIP_V1" {
		t.Fatalf("DOS primary: %+v %v", content, err)
	}
	snapshot.Source.ContentKind = "MULTI_DISC"
	snapshot.ValidationFiles = []PreviewFile{{Role: "MULTI_DISC_PLAYLIST", LogicalName: "playlist.m3u", BlobID: "playlist"}}
	snapshot.SourceFiles = []PreviewFile{{Role: "DISC", LogicalName: "disc-A.chd", BlobID: "A", SortOrder: 0}, {Role: "DISC", LogicalName: "disc-B.chd", BlobID: "B", SortOrder: 1}}
	content, err = previewContent(snapshot)
	if err != nil || content.BlobID != "playlist" || len(content.Files) != 2 || *content.Files[0].VirtualPath != "/disc-A.chd" || content.Files[1].BlobID != "B" {
		t.Fatalf("disc projection: %+v %v", content, err)
	}
	snapshot.Source.ValidationStatus = "BLOCKED"
	if _, err := previewContent(snapshot); !errors.Is(err, ErrReviewPreviewUnavailable) {
		t.Fatalf("blocked playlist: %v", err)
	}
	snapshot.Source.ValidationStatus = "READY"
	for _, count := range []int{1, 9} {
		snapshot.SourceFiles = make([]PreviewFile, 0, count)
		for index := range count {
			snapshot.SourceFiles = append(snapshot.SourceFiles, PreviewFile{Role: "DISC", LogicalName: fmt.Sprintf("disc-%d.chd", index), BlobID: "disc", SortOrder: index})
		}
		if _, err := previewContent(snapshot); !errors.Is(err, ErrReviewPreviewUnavailable) {
			t.Fatalf("%d discs accepted: %v", count, err)
		}
	}
}

func TestPreviewProjectsKeepTheirOwnPrimaryAndFileSet(t *testing.T) {
	t.Parallel()
	cases := []struct{ kind, profile, marker string }{
		{"ONS_PROJECT", `{"schemaVersion":1,"ons":{"markerPath":"0.txt","fontPath":"default.ttf","scriptEncoding":"utf8"}}`, "0.txt"},
		{"KIRIKIRI_PROJECT", `{"schemaVersion":1,"kirikiri":{"markerPath":"startup.tjs","startupXp3Path":null,"compatibility":"KAG_RUNTIME_TRIAL_REQUIRED"}}`, "startup.tjs"},
		{"NXENGINE_PROJECT", `{"schemaVersion":1,"nxengine":{"markerPath":"Doukutsu.exe","compatibility":"NXENGINE_RUNTIME_TRIAL_REQUIRED"}}`, "Doukutsu.exe"},
		{"BUTTERSCOTCH_PROJECT", `{"schemaVersion":1,"butterscotch":{"markerPath":"data.win","compatibility":"GAMEMAKER_RUNTIME_TRIAL_REQUIRED"}}`, "data.win"},
		{"TYRANOSCRIPT_PROJECT", `{"schemaVersion":1,"tyranoScript":{"entryPath":"index.html","compatibility":"TYRANOSCRIPT_RUNTIME_TRIAL_REQUIRED"}}`, "index.html"},
	}
	for _, test := range cases {
		t.Run(test.kind, func(t *testing.T) {
			snapshot := PreviewSnapshot{Source: PreviewSource{ContentKind: test.kind, DependencySnapshot: test.profile}, SourceFiles: []PreviewFile{
				{Role: "PROJECT_FILE", LogicalName: "Data/extra.bin", BlobID: "extra", SortOrder: 0},
				{Role: "PROJECT_FILE", LogicalName: test.marker, BlobID: "primary", SortOrder: 1},
			}, ValidationFiles: []PreviewFile{{Role: "PARENT", LogicalName: "unrelated.zip", BlobID: "parent"}}}
			content, err := previewContent(snapshot)
			if err != nil || content.LogicalName != test.marker || content.BlobID != "primary" || content.Format != test.kind || len(content.Files) != 1 || content.Files[0].BlobID != "extra" {
				t.Fatalf("project projection: %+v %v", content, err)
			}
			snapshot.SourceFiles = snapshot.SourceFiles[:1]
			if _, err := previewContent(snapshot); !errors.Is(err, ErrReviewPreviewUnavailable) {
				t.Fatalf("missing primary accepted: %v", err)
			}
		})
	}
}

func TestPreviewFilePolicyRejectsAmbiguousOrUnsafePaths(t *testing.T) {
	t.Parallel()
	cases := []PreviewFile{
		{Role: "PROJECT_FILE", LogicalName: "../escape", BlobID: "blob"},
		{Role: "PARENT", LogicalName: "folder/parent.zip", BlobID: "blob"},
		{Role: "PROJECT_FILE", LogicalName: "GAME.bin", BlobID: "blob"},
		{Role: "RUNTIME_FILE", LogicalName: "safe.bin", VirtualPath: new("/wrong")},
	}
	for _, file := range cases {
		if validPreviewFileSet("game.bin", []PreviewFile{file}) {
			t.Fatalf("unsafe file accepted: %q", file.LogicalName)
		}
	}
	files := []PreviewFile{{Role: "BIOS_BUNDLE", LogicalName: "a.bin", VirtualPath: new("/bios")}, {Role: "BIOS_BUNDLE", LogicalName: "b.bin", VirtualPath: new("/bios")}}
	if validPreviewFileSet("game.bin", files) {
		t.Fatal("duplicate runtime mount accepted")
	}
	files = []PreviewFile{{Role: "PROJECT_FILE", LogicalName: "Data/file.bin"}, {Role: "PROJECT_FILE", LogicalName: "data/FILE.bin"}}
	if validPreviewFileSet("game.bin", files) {
		t.Fatal("ambiguous ASCII-fold project names accepted")
	}
}
