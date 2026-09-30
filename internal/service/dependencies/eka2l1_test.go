package dependencies

import "testing"

func TestEKA2L1RequiresMatchingROMAndRPKG(t *testing.T) {
	expected := map[string]int64{"Nokia5320.rom": 69947392, "Nokia5320.rpkg": 121092775}
	found := 0
	for _, requirement := range staticBIOSCatalog {
		if requirement.coreID != "eka2l1" {
			continue
		}
		found++
		if requirement.mode != "REQUIRED" || requirement.delivery != "EXTERNAL_FILE" ||
			requirement.emulatorPath != "/"+requirement.logical || expected[requirement.logical] != requirement.size || len(requirement.sha256) != 64 ||
			requirement.providerID != "retrom-runtime" || requirement.targetID != "symbian-eka2l1" {
			t.Fatalf("invalid firmware: %+v", requirement)
		}
		if err := validateBIOSDelivery(requirement); err != nil {
			t.Fatal(err)
		}
	}
	if found != 2 {
		t.Fatalf("expected paired firmware, found %d", found)
	}
}
