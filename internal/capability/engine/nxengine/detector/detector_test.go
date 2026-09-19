package detector

import (
	"errors"
	"testing"
)

func completeFiles(exeSize int64) []File {
	return []File{
		{Path: "Doukutsu.exe", Size: exeSize},
		{Path: "data/npc.tbl", Size: 1},
		{Path: "data/Stage/Start.pxm", Size: 1},
		{Path: "data/Stage/Start.tsc", Size: 1},
	}
}

func TestSelectAndDetectRequireCompleteGame(t *testing.T) {
	t.Parallel()
	selection, err := Select(completeFiles(128))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := Detect(selection, [2]byte{'M', 'Z'})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := MarshalSnapshot(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSnapshot(string(snapshot)); err != nil {
		t.Fatal(err)
	}
	if selection.ProbePath() != "Doukutsu.exe" {
		t.Fatalf("ProbePath() = %q", selection.ProbePath())
	}
}

func TestSelectRejectsAmbiguousAndBoundedDescriptors(t *testing.T) {
	t.Parallel()
	duplicate := append(completeFiles(128), File{Path: "doukutsu.exe", Size: 128})
	for _, files := range [][]File{
		duplicate,
		completeFiles(127),
		completeFiles(16*1024*1024 + 1),
	} {
		if _, err := Select(files); !errors.Is(err, ErrProjectInvalid) {
			t.Fatalf("Select(%d files) error=%v", len(files), err)
		}
	}
}

func TestDetectChecksEXEBeforeRequiredAssets(t *testing.T) {
	t.Parallel()
	selection, err := Select([]File{{Path: "Doukutsu.exe", Size: 128}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Detect(selection, [2]byte{'N', 'O'}); !errors.Is(err, ErrProjectInvalid) {
		t.Fatalf("wrong MZ error=%v", err)
	}
	if _, err := Detect(selection, [2]byte{'M', 'Z'}); !errors.Is(err, ErrProjectInvalid) {
		t.Fatalf("missing assets error=%v", err)
	}
}
