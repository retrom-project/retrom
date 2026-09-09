package contentprofile

import "testing"

func TestPS2AcceptsOnlySingleDiscImages(t *testing.T) {
	for _, name := range []string{"disc.ISO", "disc.chd"} {
		if !AcceptsRaw("ps2", name) {
			t.Errorf("PS2 rejected %q", name)
		}
	}
	for _, name := range []string{"disc.bin", "disc.cue", "playlist.m3u", "game.elf", "disc.iso.zip"} {
		if AcceptsRaw("ps2", name) {
			t.Errorf("PS2 accepted unsupported %q", name)
		}
	}
}
