package contentprofile

import "testing"

func TestPlatformExpansionContentAdmission(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"gamegear": {"cart.gg"}, "sg1000": {"cart.sg"}, "multivision": {"cart.sg"},
		"pico": {"cart.md", "cart.bin"}, "sega32x": {"cart.32x"},
		"supergrafx": {"cart.pce", "cart.sgx"}, "gx4000": {"cart.cpr"},
	}
	for platform, names := range cases {
		for _, name := range names {
			if !AcceptsRaw(platform, name) {
				t.Errorf("%s must accept %s", platform, name)
			}
		}
		for _, name := range []string{"cart.exe", "cart.chd", "cart.m3u", "cart.gg.bak"} {
			if AcceptsRaw(platform, name) {
				t.Errorf("%s must reject %s", platform, name)
			}
		}
		for _, format := range []ArchiveFormat{ArchiveZIP, ArchiveSevenZip} {
			if !AcceptsArchive(platform, format) {
				t.Errorf("%s must accept single-member archives", platform)
			}
		}
	}
}
