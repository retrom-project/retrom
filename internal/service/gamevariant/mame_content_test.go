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
