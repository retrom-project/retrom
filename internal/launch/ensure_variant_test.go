package launch

import "testing"

func TestValidateLockedArcadeSnapshotKeepsCurrentTypedClosure(t *testing.T) {
	t.Parallel()
	valid := `{"schemaVersion":1,"kind":"ARCADE","machine":"pacman","datVersionId":"dat-v2","closure":[],"dependencies":[{"kind":"PARENT","machine":"puckman","state":"SATISFIED_EXTERNAL"},{"kind":"BIOS_OR_BASE","machine":"retrombios","state":"SATISFIED_EXTERNAL"}],"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`
	if err := validateLockedArcadeSnapshot(valid, "pacman.zip", "dat-v2"); err != nil {
		t.Fatalf("valid Arcade snapshot: %v", err)
	}
	invalid := map[string]string{
		"static BIOS schema": `{"schemaVersion":1,"kind":"STATIC","bios":[]}`,
		"wrong DAT":          `{"schemaVersion":1,"kind":"ARCADE","machine":"pacman","datVersionId":"other","closure":[],"dependencies":[],"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`,
		"wrong machine":      `{"schemaVersion":1,"kind":"ARCADE","machine":"puckman","datVersionId":"dat-v2","closure":[],"dependencies":[],"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`,
	}
	for name, raw := range invalid {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := validateLockedArcadeSnapshot(raw, "pacman.zip", "dat-v2"); err == nil {
				t.Fatal("invalid snapshot was accepted")
			}
		})
	}
}
