package gamevariant

import (
	"errors"
	"testing"

	"retrom/internal/content/arcade"
	corevalidation "retrom/internal/core/validation"
)

func TestAlternateArcadeAcceptsOnlySeparatelyInstalledBIOSGap(t *testing.T) {
	t.Parallel()
	dat := "current-dat"
	source := Source{ActiveDATVersionID: &dat, ValidationLogicalName: "sample.zip"}
	cases := []struct {
		name, status, code, dependency, machine, missing string
		accepted                                         bool
	}{
		{"BIOS installed separately", "BLOCKED", "LAUNCH_BIOS_MISSING", "BIOS_OR_BASE", "bios", "bios.zip", true},
		{"parent absent", "BLOCKED", "LAUNCH_PARENT_MISSING", "PARENT", "parent", "parent.zip", false},
		{"content missing", "BLOCKED", "ARCADE_CONTENT_MISSING_ENTRY", "BIOS_OR_BASE", "bios", "rom.bin", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := arcade.Result{
				Status: test.status, Code: test.code,
			}
			encoded := `{"schemaVersion":1,"kind":"ARCADE","machine":"sample","datVersionId":"current-dat",` +
				`"closure":[],"dependencies":[{"kind":"` + test.dependency + `","machine":"` + test.machine +
				`","state":"MISSING","requiredEntries":[]}],"missingEntries":["` + test.missing +
				`"],"mismatchedEntries":[],"warnings":[]}`
			result.Snapshot, _ = arcade.ParseSnapshot(encoded)
			prepared, err := preparedArcadeResult(result, source)
			if test.accepted {
				if err != nil || prepared == nil || len(prepared.Dependencies) != 1 {
					t.Fatalf("prepared=%v error=%v", prepared, err)
				}
			} else if !errors.Is(err, ErrBlocked) {
				t.Fatalf("error=%v, want blocked", err)
			}
		})
	}
}

func TestAlternateArcadeBIOSUsesItsOwnInstallation(t *testing.T) {
	t.Parallel()
	file, matched := "current-bios-blob", "MATCHED"
	facts := BIOSFacts{Arcade: []ArcadeBIOS{{
		State: "MISSING", CatalogPresent: true,
		Dependency: corevalidation.BIOSDependency{
			BIOSCatalogEntry: corevalidation.BIOSCatalogEntry{
				LogicalName: "bios.zip", DeliveryKind: "BIOS_BUNDLE", RequirementMode: "REQUIRED",
			},
			FileRecord: &file, InstallationStatus: &matched,
		},
	}}}
	_, status, _, err := ResolveBIOS(Source{ProviderID: "retrom-runtime", TargetID: "mame-arcade"}, facts, "sample.zip")
	if err != nil || status != "READY" {
		t.Fatalf("installed Current BIOS: status=%s error=%v", status, err)
	}
	facts.Arcade[0].Dependency.FileRecord = nil
	_, status, code, err := ResolveBIOS(Source{ProviderID: "retrom-runtime", TargetID: "mame-arcade"}, facts, "sample.zip")
	if err != nil || status != "BLOCKED" || code != "LAUNCH_BIOS_MISSING" {
		t.Fatalf("missing Current BIOS: status=%s code=%s error=%v", status, code, err)
	}
}
