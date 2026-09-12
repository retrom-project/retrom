package contentprofile

import "testing"

func TestPC88DiskProfile(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"game.u88", "GAME.U88", "boot.d88"} {
		if !AcceptsRaw("pc88", name) {
			t.Errorf("PC-88 disk rejected: %s", name)
		}
	}
	for _, name := range []string{"game.exe", "game.iso", "game.u88.exe", "disks.m3u"} {
		if AcceptsRaw("pc88", name) {
			t.Errorf("unsupported PC-88 input accepted: %s", name)
		}
	}
}
