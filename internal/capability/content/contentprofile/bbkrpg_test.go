package contentprofile

import "testing"

func TestBBKRPGSingleGameAdmission(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"game.gam", "GAME.GAM", "伏魔记.gam"} {
		if !AcceptsRaw("bbkrpg", name) {
			t.Errorf("BBK RPG must accept %s", name)
		}
	}
	for _, name := range []string{"8.BIN", "game.gam.exe", "game.nes"} {
		if AcceptsRaw("bbkrpg", name) {
			t.Errorf("BBK RPG must reject %s", name)
		}
	}
	if !AllowsContentKind("bbkrpg", ContentKindSingleFile) || AllowsContentKind("bbkrpg", ContentKindMultiDisc) {
		t.Fatal("BBK RPG requires one GAM image")
	}
}
