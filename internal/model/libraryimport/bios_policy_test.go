package libraryimport

import "testing"

func TestBIOSContentLogicalNamePreservesSourceOrderAndDOSFallback(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, platform, fallback, want string
		sources                        []PreparedSource
	}{
		{"content", "gba", "", "game.gba", []PreparedSource{{Role: "COVER", LogicalName: "cover.png"}, {Role: "CONTENT", LogicalName: "game.gba"}}},
		{"first disc", "psx", "", "disc1.chd", []PreparedSource{{Role: "DISC", LogicalName: "disc1.chd"}, {Role: "CONTENT", LogicalName: "disc2.chd"}}},
		{"first empty content is authoritative", "gba", "", "", []PreparedSource{{Role: "CONTENT"}, {Role: "DISC", LogicalName: "later.chd"}}},
		{"DOS missing source fallback", "dos", "play.exe", "play.exe", nil},
		{"DOS empty first content fallback", "dos", "play.exe", "play.exe", []PreparedSource{{Role: "CONTENT"}, {Role: "DISC", LogicalName: "later.exe"}}},
		{"DOS content precedes fallback", "dos", "fallback.exe", "first.exe", []PreparedSource{{Role: "CONTENT", LogicalName: "first.exe"}}},
		{"non-DOS ignores fallback", "gba", "play.exe", "", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			group := PreparedGroup{Sources: test.sources, DefaultDOSEntry: test.fallback}
			if got := group.BIOSContentLogicalName(test.platform); got != test.want {
				t.Fatalf("name=%q want=%q", got, test.want)
			}
		})
	}
}
