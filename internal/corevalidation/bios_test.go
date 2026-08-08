package corevalidation

import "testing"

func TestBIOSAppliesUsesOnlyCanonicalContentSuffix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		condition, name string
		want            bool
	}{
		{"FDS_CONTENT", "Disk.FDS", true},
		{"FDS_CONTENT", "Disk.fds.zip", false},
		{"GB_CONTENT", "Game.dmg", true},
		{"GBC_CONTENT", "Game.GBC", true},
		{"GBA_CONTENT", "Game.gba", true},
		{"GAME_GENIE_ADDON_MODE", "Game.nes", false},
		{"", "Game.chd", true},
	}
	for _, test := range tests {
		if actual := BIOSApplies(test.condition, test.name); actual != test.want {
			t.Errorf("BIOSApplies(%q, %q) = %t", test.condition, test.name, actual)
		}
	}
}
