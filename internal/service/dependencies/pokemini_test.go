package dependencies

import "testing"

func TestPokemonMiniRequiresIndependentBIOSFiles(t *testing.T) {
	expected := map[string]int64{"bios.min": 4096}
	found := 0
	for _, requirement := range staticBIOSCatalog {
		if requirement.coreID != "gbe_plus" {
			continue
		}
		found++
		if requirement.mode != "REQUIRED" || requirement.delivery != "EXTERNAL_FILE" ||
			requirement.emulatorPath != "/"+requirement.logical ||
			expected[requirement.logical] != requirement.size || len(requirement.sha256) != 64 {
			t.Fatalf("invalid PokemonMini requirement: %+v", requirement)
		}
		if err := validateBIOSDelivery(requirement); err != nil {
			t.Fatal(err)
		}
	}
	if found != len(expected) {
		t.Fatalf("PokemonMini BIOS count: %d", found)
	}
}
