package contentprofile

import "testing"

func TestNeoGeoCDOnlyAcceptsCHD(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"game.chd", "game.CHD"} {
		if !AcceptsRaw("neogeocd", name) {
			t.Errorf("Neo Geo CD must accept %s", name)
		}
	}
	for _, name := range []string{"game.cue", "game.bin", "game.iso", "game.zip", "game.m3u"} {
		if AcceptsRaw("neogeocd", name) {
			t.Errorf("Neo Geo CD must reject %s", name)
		}
	}
}
