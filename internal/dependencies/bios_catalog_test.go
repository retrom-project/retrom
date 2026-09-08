package dependencies

import "testing"

func TestNewEmulatorJSCoresDeclareRequiredStaticBIOS(t *testing.T) {
	t.Parallel()
	want := map[string]staticBIOS{
		"gearcoleco": {
			coreID: "gearcoleco", logical: "colecovision.rom", mode: "REQUIRED", size: 8192,
			md5: "2c66f5911e5b42b8ebe113403548eee7", sha256: "990bf1956f10207d8781b619eb74f89b00d921c8d45c95c334c16c8cceca09ad",
			sourceURL: "https://docs.libretro.com/library/gearcoleco/",
		},
		"prboom": {
			coreID: "prboom", logical: "prboom.wad", mode: "REQUIRED", size: 143312,
			md5: "72ae1b47820fcc93cc0df9c428d0face", sha256: "b4dd3642932193cc42bca0ee98bf30004888ca4850d69e85023b8baacfba1d1d",
			sourceURL: "https://docs.libretro.com/library/prboom/",
		},
	}
	for _, requirement := range staticBIOSCatalog {
		expected, ok := want[requirement.coreID]
		if !ok {
			continue
		}
		if requirement != expected {
			t.Errorf("static BIOS for %q = %#v, want %#v", requirement.coreID, requirement, expected)
		}
		delete(want, requirement.coreID)
	}
	for coreID := range want {
		t.Errorf("static BIOS for %q is missing", coreID)
	}
}
