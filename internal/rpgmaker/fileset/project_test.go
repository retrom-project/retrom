package fileset

import (
	"errors"
	"testing"

	"retrom/internal/importing"
	"retrom/internal/rpgmaker/detector"
	"retrom/internal/testassert"
)

func TestNormalizeProjectDropsNoiseAndOneWrapper(t *testing.T) {
	project, err := NormalizeProject([]SourceFile{
		{Path: "KnightBlade/Game.ini", SizeBytes: 1, SourceIndex: 1},
		{Path: "KnightBlade/Data/Scripts.rxdata", SizeBytes: 2, SourceIndex: 2},
		{Path: "KnightBlade/.DS_Store", SourceIndex: 3},
		{Path: "__MACOSX/ignored", SourceIndex: 4},
	})
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return project.Root != "." },
		func() bool { return len(project.Files) != 2 },
		func() bool { return project.Files[0].Path != "Data/Scripts.rxdata" },
		func() bool { return project.Files[1].Path != "Game.ini" },
		func() bool { return len(project.RemovedNoise) != 2 },
	), "project=%#v error=%v", project, err)
}

func TestNormalizeProjectSelectsOnlyWWWAndExcludesWrapper(t *testing.T) {
	project, err := NormalizeProject([]SourceFile{
		{Path: "Game.exe"},
		{Path: "www/index.html"},
		{Path: "www/data/System.json"},
		{Path: "www/js/rpg_core.js"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if project.Root != "www" || len(project.Files) != 3 {
		t.Fatalf("project=%#v", project)
	}
	for _, file := range project.Files {
		if file.Path == "Game.exe" || len(file.Path) >= 4 && file.Path[:4] == "www/" {
			t.Fatalf("wrapper leaked: %#v", project.Files)
		}
	}
}

func TestNormalizeProjectRejectsAmbiguousAndCollidingRoots(t *testing.T) {
	tests := []struct {
		name  string
		files []SourceFile
		code  Code
	}{
		{name: "two roots", files: []SourceFile{{Path: "Game.ini"}, {Path: "www/index.html"}, {Path: "www/data/System.json"}}, code: CodeRootAmbiguous},
		{name: "unicode collision", files: []SourceFile{{Path: "Game.ini"}, {Path: "Ｄata/a"}, {Path: "data/A"}}, code: CodePathCollision},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeProject(test.files)
			var projectError *ProjectError
			if !errors.As(err, &projectError) || projectError.Code != test.code {
				t.Fatalf("error=%v, want %s", err, test.code)
			}
		})
	}
}

func TestNormalizeProjectKeepsNestedArchivesOpaque(t *testing.T) {
	t.Parallel()
	input := []SourceFile{
		{Path: "RPG_RT.ldb"},
		{Path: "RPG_RT.lmt"},
		{
			Path: "RPG_RT.exe.7z", SizeBytes: 301773,
			NestedArchiveFormat: importing.NestedArchiveSevenZip,
		},
		{
			Path: "Data/assets.zip", SizeBytes: 1024,
			NestedArchiveFormat: importing.NestedArchiveZIP,
		},
	}
	project, err := NormalizeProject(input)
	if err != nil || len(project.Files) != len(input) {
		t.Fatalf("opaque nested project=%#v error=%v", project, err)
	}
	formats := map[string]importing.NestedArchiveFormat{}
	for _, file := range project.Files {
		formats[file.Path] = file.NestedArchiveFormat
	}
	if formats["RPG_RT.exe.7z"] != importing.NestedArchiveSevenZip ||
		formats["Data/assets.zip"] != importing.NestedArchiveZIP {
		t.Fatalf("opaque classifications were not retained: %#v", formats)
	}
}

func TestExcludeSessionStateUsesGenerationSpecificRootRules(t *testing.T) {
	files := []SourceFile{
		{Path: "Save01.lsd"},
		{Path: "Save01.dyn"},
		{Path: "Save1.rxdata"},
		{Path: "Data/Save01.lsd"},
		{Path: "RPG_RT.ldb"},
	}
	included, excluded := ExcludeSessionState(detector.RPG2000, files)
	if len(excluded) != 2 || len(included) != 3 {
		t.Fatalf("included=%#v excluded=%#v", included, excluded)
	}
	included, excluded = ExcludeSessionState(detector.RPGXP, files)
	if len(excluded) != 1 || excluded[0] != "Save1.rxdata" || len(included) != 4 {
		t.Fatalf("included=%#v excluded=%#v", included, excluded)
	}
}
