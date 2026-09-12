package dependencies

import "testing"

func TestNeoCDRequiresOriginalCDZBIOSAtNativePath(t *testing.T) {
	t.Parallel()
	for _, item := range staticBIOSCatalog {
		if item.coreID != "neocd" {
			continue
		}
		if item.mode != "REQUIRED" || item.logical != "neocd.bin" || item.size != 524288 ||
			item.delivery != "EXTERNAL_FILE" || item.emulatorPath != "/neocd/neocd.bin" ||
			item.sha256 != "2e93af5848080ea04d17a7841b742f009330d30e4ff40c3410547581d921c892" {
			t.Fatalf("unexpected NeoCD BIOS declaration: %#v", item)
		}
		return
	}
	t.Fatal("NeoCD BIOS requirement missing")
}
