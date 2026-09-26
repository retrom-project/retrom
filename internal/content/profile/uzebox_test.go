package contentprofile

import "testing"

func TestUzeboxSingleCartridgeAdmission(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"game.uze", "GAME.UZE"} {
		if !AcceptsRaw("uzebox", name) {
			t.Errorf("Uzebox must accept %s", name)
		}
	}
	for _, name := range []string{"game.hex", "game.bin", "game.uze.exe"} {
		if AcceptsRaw("uzebox", name) {
			t.Errorf("Uzebox must reject %s", name)
		}
	}
	if !AllowsContentKind("uzebox", ContentKindSingleFile) || AllowsContentKind("uzebox", ContentKindMultiDisc) {
		t.Fatal("Uzebox requires a single cartridge")
	}
}
