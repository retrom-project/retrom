package contentprofile

import (
	"errors"
	"testing"

	"retrom/internal/importing"
)

func TestPX68KSingleDiskAdmission(t *testing.T) {
	for _, name := range []string{"game.DIM", "game.xdf", "game.hdf"} {
		if !AcceptsRaw("x68000", name) {
			t.Errorf("rejected %s", name)
		}
	}
	for _, name := range []string{"game.d88", "game.m3u", "game.zip"} {
		if AcceptsRaw("x68000", name) {
			t.Errorf("accepted %s", name)
		}
	}
	if !AcceptsArchive("x68000", ArchiveZIP) {
		t.Fatal("single-disk archive rejected")
	}
	_, err := SelectArchivePrimary("x68000", []importing.ArchiveEntry{{NormalizedPath: "disk1.dim"}, {NormalizedPath: "disk2.dim"}})
	if !errors.Is(err, ErrAmbiguousPrimaryContent) {
		t.Fatalf("multiple disks: %v", err)
	}
}
