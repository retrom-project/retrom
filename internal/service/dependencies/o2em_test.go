package dependencies

import "testing"

func TestO2EMRequiresUserSuppliedFirmware(t *testing.T) {
	t.Parallel()
	count := 0
	for _, item := range staticBIOSCatalog {
		if item.coreID != "o2em" {
			continue
		}
		count++
		if item.logical != "o2rom.bin" || item.mode != "REQUIRED" || item.size != 1024 ||
			item.md5 != "562d5ebf9e030a40d6fabfc2f33139fd" || item.delivery != "" {
			t.Errorf("invalid O2EM firmware requirement: %#v", item)
		}
	}
	if count != 1 {
		t.Fatalf("want one O2EM firmware requirement, got %d", count)
	}
}
