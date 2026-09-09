package contentprofile

import "testing"

func TestPC98DiskProfile(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"game.hdi", "GAME.HDI", "boot.d88"} {
		if !AcceptsRaw("pc98", name) {
			t.Errorf("PC-98 disk rejected: %s", name)
		}
	}
	for _, name := range []string{"game.exe", "game.iso", "game.hdi.exe", "disks.m3u"} {
		if AcceptsRaw("pc98", name) {
			t.Errorf("unsupported PC-98 input accepted: %s", name)
		}
	}
}
