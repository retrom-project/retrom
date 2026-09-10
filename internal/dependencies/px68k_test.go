package dependencies

import "testing"

func TestPX68KRequiresIndependentBIOSFiles(t *testing.T) {
	expected := map[string]int64{"iplrom.dat": 131072, "cgrom.dat": 786432}
	found := 0
	for _, requirement := range staticBIOSCatalog {
		if requirement.coreID != "px68k" {
			continue
		}
		found++
		if requirement.mode != "REQUIRED" || requirement.delivery != "EXTERNAL_FILE" ||
			requirement.emulatorPath != "/game/keropi/"+requirement.logical ||
			expected[requirement.logical] != requirement.size || len(requirement.sha256) != 64 {
			t.Fatalf("invalid PX68K requirement: %+v", requirement)
		}
		if err := validateBIOSDelivery(requirement); err != nil {
			t.Fatal(err)
		}
	}
	if found != len(expected) {
		t.Fatalf("PX68K BIOS count: %d", found)
	}
}
