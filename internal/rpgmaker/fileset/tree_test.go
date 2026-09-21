package fileset

import "testing"

func TestNormalizeTreePreservesRootsWithoutGameMarkers(t *testing.T) {
	project, err := NormalizeTree([]SourceFile{{Path: "Game/sky.dsk", SizeBytes: 4, SourceIndex: 1}, {Path: "Other/story", SizeBytes: 4, SourceIndex: 2}})
	if err != nil || len(project.Files) != 2 || project.Files[0].Path != "Game/sky.dsk" || project.Files[1].Path != "Other/story" {
		t.Fatalf("tree=%+v err=%v", project, err)
	}
	for _, paths := range [][]SourceFile{
		{{Path: "Game/sky.dsk"}, {Path: "game/SKY.DSK"}}, {{Path: "../escape"}}, {{Path: "a"}, {Path: "a/b"}},
	} {
		if _, err := NormalizeTree(paths); err == nil {
			t.Fatalf("unsafe tree accepted: %+v", paths)
		}
	}
}
