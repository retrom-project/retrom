package gamevariant

import "testing"

func TestMameApplePilotRejectsUnsupportedDisks(t *testing.T) {
	for _, test := range []struct {
		name  string
		size  int64
		ready bool
	}{
		{"game.dsk", 143360, true},
		{"game.DO", 143360, true},
		{"game.po", 143360, false},
		{"game.woz", 143360, false},
		{"game.dsk", 143359, false},
		{"game.dsk", 143361, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts := ValidationFacts{BindingFound: true, RelationshipEnabled: true, Content: Snapshot{
				Source:    Source{ProviderID: "retrom-runtime", TargetID: "mame-apple2", PlatformID: "apple2", ContentKind: "SINGLE_FILE", ValidationLogicalName: test.name},
				GameFiles: []File{{Role: "PRIMARY", LogicalName: test.name, SizeBytes: test.size}},
			}}
			status, code := validationContentStatus(facts)
			if (status == "READY") != test.ready || !test.ready && code != "CORE_CONTENT_FORMAT_UNSUPPORTED" {
				t.Fatalf("status %s code %s", status, code)
			}
		})
	}
}

func TestMameAdditionalMachinesContentAdmission(t *testing.T) {
	for _, test := range []struct {
		platform, name string
		size           int64
		ready          bool
	}{
		{"atom", "game.atm", 14933, true},
		{"atom", "game.ATM", 23, true},
		{"atom", "game.uef", 14933, false},
		{"atom", "game.atm", 22, false},
		{"atom", "game.atm", 65558, false},
		{"pv1000", "game.rom", 8192, true},
		{"pv1000", "game.BIN", 16384, true},
		{"pv1000", "game.bin", 32768, true},
		{"pv1000", "game.bin", 8193, false},
		{"pv1000", "game.zip", 8192, false},
	} {
		facts := ValidationFacts{BindingFound: true, RelationshipEnabled: true, Content: Snapshot{
			Source: Source{
				ProviderID: "retrom-runtime", TargetID: "mame-" + test.platform,
				PlatformID: test.platform, ContentKind: "SINGLE_FILE", ValidationLogicalName: test.name,
			},
			GameFiles: []File{{Role: "PRIMARY", LogicalName: test.name, SizeBytes: test.size}},
		}}
		status, code := validationContentStatus(facts)
		if (status == "READY") != test.ready || !test.ready && code != "CORE_CONTENT_FORMAT_UNSUPPORTED" {
			t.Errorf("%s/%s/%d: status %s code %s", test.platform, test.name, test.size, status, code)
		}
	}
}
