package gamevariant

import "testing"

func TestMameApplePilotRejectsUnsupportedDisks(t *testing.T) {
	for _, test := range []struct {
		target string
		name   string
		size   int64
		ready  bool
	}{
		{"mame-apple2", "game.dsk", 143360, true},
		{"mame-apple2e", "game.DO", 143360, true},
		{"mame-apple2e", "game.po", 143360, false},
		{"mame-apple2", "game.woz", 143360, false},
		{"mame-apple2e", "game.dsk", 143359, false},
		{"mame-apple2", "game.dsk", 143361, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts := ValidationFacts{BindingFound: true, RelationshipEnabled: true, Content: Snapshot{
				Source:    Source{ProviderID: "retrom-runtime", TargetID: test.target, PlatformID: "apple2", ContentKind: "SINGLE_FILE", ValidationLogicalName: test.name},
				GameFiles: []File{{Role: "PRIMARY", LogicalName: test.name, SizeBytes: test.size}},
			}}
			status, code := validationContentStatus(facts)
			if (status == "READY") != test.ready || !test.ready && code != "CORE_CONTENT_FORMAT_UNSUPPORTED" {
				t.Fatalf("status %s code %s", status, code)
			}
		})
	}
}

func TestMameCartridgesAcceptBoundedMedia(t *testing.T) {
	for _, test := range []struct {
		platform, name string
		size           int64
		ready          bool
	}{
		{"sg1000", "game.sg", 49152, true},
		{"sg1000", "game.zip", 4096, false},
		{"sg1000", "game.col", 32768, false},
		{"sg1000", "game.sg", 49153, false},
		{"colecovision", "game.col", 32768, true},
		{"colecovision", "game.zip", 4096, false},
		{"colecovision", "game.sg", 32768, false},
		{"colecovision", "game.col", 65536, false},
	} {
		target := "mame-sg1000"
		if test.platform == "colecovision" {
			target = "mame-coleco"
		}
		facts := ValidationFacts{BindingFound: true, RelationshipEnabled: true, Content: Snapshot{
			Source: Source{
				ProviderID: "retrom-runtime", TargetID: target,
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
