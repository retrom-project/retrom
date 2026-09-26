package contentprofile

import "testing"

func TestOdyssey2SingleCartridgeAdmission(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"game.bin", "GAME.BIN"} {
		if !AcceptsRaw("odyssey2", name) {
			t.Errorf("Odyssey2 must accept %s", name)
		}
	}
	for _, name := range []string{"game.rom", "game.zip.exe"} {
		if AcceptsRaw("odyssey2", name) {
			t.Errorf("Odyssey2 must reject %s", name)
		}
	}
	if !AllowsContentKind("odyssey2", ContentKindSingleFile) || AllowsContentKind("odyssey2", ContentKindMultiDisc) {
		t.Fatal("Odyssey2 requires one cartridge")
	}
}
