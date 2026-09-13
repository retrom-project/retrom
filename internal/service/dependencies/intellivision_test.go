package dependencies

import "testing"

func TestIntellivisionRequiresBothFirmwareFiles(t *testing.T) {
	t.Parallel()
	want := map[string]int64{"exec.bin": 8192, "grom.bin": 2048}
	for _, item := range staticBIOSCatalog {
		if item.coreID != "freeintv" {
			continue
		}
		size, ok := want[item.logical]
		if !ok || item.size != size || item.mode != "REQUIRED" || len(item.md5) != 32 || len(item.sha256) != 64 {
			t.Errorf("invalid Intellivision firmware requirement: %#v", item)
		}
		delete(want, item.logical)
	}
	if len(want) != 0 {
		t.Errorf("missing Intellivision firmware: %v", want)
	}
}
