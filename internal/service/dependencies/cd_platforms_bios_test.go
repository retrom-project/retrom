package dependencies

import "testing"

func TestDiscPlatformsDeclareRegionAndMachineFirmware(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"genesis_plus_gx/bios_CD_E.bin": "SEGA_CD_CONTENT",
		"genesis_plus_gx/bios_CD_U.bin": "SEGA_CD_CONTENT",
		"genesis_plus_gx/bios_CD_J.bin": "SEGA_CD_CONTENT",
		"puae/kick40060.CD32":           "AMIGA_CD32_CONTENT",
		"puae/kick40060.CD32.ext":       "AMIGA_CD32_CONTENT",
	}
	for _, requirement := range staticBIOSCatalog {
		key := requirement.coreID + "/" + requirement.logical
		condition, ok := want[key]
		if !ok {
			continue
		}
		if requirement.mode != "CONDITIONAL" || requirement.condition != condition || requirement.size <= 0 || requirement.md5 == "" {
			t.Errorf("%s requirement = %#v", key, requirement)
		}
		delete(want, key)
	}
	for key := range want {
		t.Errorf("missing %s BIOS requirement", key)
	}
}
