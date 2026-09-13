package dependencies

import "testing"

func TestPC88RequiresIndependentBIOSFiles(t *testing.T) {
	expected := map[string]int64{"N88.ROM": 32768, "N88EXT0.ROM": 8192, "N88EXT1.ROM": 8192, "N88EXT2.ROM": 8192, "N88EXT3.ROM": 8192, "N88N.ROM": 32768, "N88SUB.ROM": 2048}
	found := 0
	for _, requirement := range staticBIOSCatalog {
		if requirement.coreID != "quasi88" {
			continue
		}
		found++
		if requirement.mode != "REQUIRED" || requirement.delivery != "EXTERNAL_FILE" ||
			requirement.emulatorPath != "/retroarch/userdata/system/quasi88/"+requirement.logical ||
			expected[requirement.logical] != requirement.size || len(requirement.sha256) != 64 {
			t.Fatalf("invalid PC88 requirement: %+v", requirement)
		}
		if err := validateBIOSDelivery(requirement); err != nil {
			t.Fatal(err)
		}
	}
	if found != len(expected) {
		t.Fatalf("PC88 BIOS count: %d", found)
	}
}
