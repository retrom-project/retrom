package contentprofile

import "testing"

func TestVectrexSingleCartridgeAdmission(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"game.vec", "GAME.VEC", "game.bin"} {
		if !AcceptsRaw("vectrex", name) {
			t.Errorf("Vectrex must accept %s", name)
		}
	}
	for _, name := range []string{"game.rom", "game.chd", "game.vec.exe"} {
		if AcceptsRaw("vectrex", name) {
			t.Errorf("Vectrex must reject %s", name)
		}
	}
	if !AcceptsArchive("vectrex", ArchiveZIP) || !AcceptsArchive("vectrex", ArchiveSevenZip) ||
		!AllowsContentKind("vectrex", ContentKindSingleFile) || AllowsContentKind("vectrex", ContentKindMultiDisc) {
		t.Fatal("Vectrex must use the existing single-cartridge archive policy")
	}
}
