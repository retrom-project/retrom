package contentprofile

import "testing"

func TestRemainingEmulatorJSContentAdmission(t *testing.T) {
	t.Parallel()
	for platform, names := range map[string][]string{
		"zx81":       {"game.p", "game.tzx", "game.t81"},
		"ngpc":       {"game.ngp", "game.ngc", "game.NGC"},
		"amstradcpc": {"game.dsk", "game.sna"},
		"pet":        {"game.prg", "game.d64"}, "plus4": {"game.prg", "game.d64"},
		"cdi": {"game.chd"}, "pcecd": {"game.chd"},
	} {
		for _, name := range names {
			if !AcceptsRaw(platform, name) {
				t.Errorf("%s must accept %s", platform, name)
			}
		}
	}
	for platform, names := range map[string][]string{
		"ngpc": {"game.ngpc"},
		"zx81": {"game.z80"}, "amstradcpc": {"discs.m3u", "game.kcr", "game.cpr"},
		"pet": {"discs.m3u", "commands.cmd"}, "plus4": {"discs.vfl"},
		"cdi": {"game.cue", "game.zip"}, "pcecd": {"game.pce", "game.cue", "game.zip"},
		"pce": {"game.chd"},
	} {
		for _, name := range names {
			if AcceptsRaw(platform, name) {
				t.Errorf("%s must reject %s", platform, name)
			}
		}
	}
}
