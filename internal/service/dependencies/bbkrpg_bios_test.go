package dependencies

import "testing"

func TestBBKRPGRequiresBothBIOSBanksAtNativePaths(t *testing.T) {
	t.Parallel()
	expected := map[string]string{
		"8.BIN": "7663735609c416025b2738c80cedaf11528ff8cb5a7c74c7c31c1f46e43e9caf",
		"E.BIN": "3e12d40948fd50710cef8c6d14acea26ad69f47312125a61bd1003912893fcad",
	}
	for _, item := range staticBIOSCatalog {
		if item.coreID != "gam4980" {
			continue
		}
		digest, exists := expected[item.logical]
		if !exists || item.mode != "REQUIRED" || item.size != 2097152 ||
			item.delivery != "EXTERNAL_FILE" || item.emulatorPath != "/gam4980/"+item.logical ||
			item.sha256 != digest {
			t.Fatalf("unexpected BBK RPG BIOS declaration: %#v", item)
		}
		delete(expected, item.logical)
	}
	if len(expected) != 0 {
		t.Fatalf("missing BBK RPG BIOS banks: %#v", expected)
	}
}
