package contentprofile

import "testing"

func TestMSXMediaProfile(t *testing.T) {
	for _, name := range []string{"Game.rom", "Game.MX1", "Game.mx2", "Disk.dsk", "Tape.cas"} {
		if !AcceptsRaw("msx", name) {
			t.Fatalf("MSX must accept %s", name)
		}
	}
	if AcceptsRaw("msx", "game.exe") || !AcceptsArchive("msx", ArchiveZIP) || !AcceptsArchive("msx", ArchiveSevenZip) {
		t.Fatal("MSX archive profile must select a single supported medium")
	}
}
