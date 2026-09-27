package contentprofile

import (
	"slices"
	"testing"
)

func TestDiscAndBroadcastPlatformsAcceptSupportedSingleFiles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		platform string
		accepted []string
		rejected []string
	}{
		{"segacd", []string{"Sonic.CHD"}, []string{"Sonic.cue", "Sonic.md", "Sonic.chd.zip"}},
		{"amigacd32", []string{"Alien.chd", "Alien.ISO"}, []string{"Alien.lha", "Alien.adf", "Alien.cue"}},
		{"satellaview", []string{"BS F-Zero.bs", "BS F-Zero.sfc", "BS F-Zero.smc"}, []string{"BS F-Zero.st", "BS F-Zero.zip"}},
	} {
		if profile, ok := ByPlatform(tc.platform); !ok || profile.ArchivePolicy != ArchiveNone || !slices.Equal(profile.ContentKinds, []ContentKind{ContentKindSingleFile}) {
			t.Errorf("%s profile = %#v, found = %t", tc.platform, profile, ok)
		}
		for _, name := range tc.accepted {
			if !AcceptsRaw(tc.platform, name) {
				t.Errorf("%s rejected %s", tc.platform, name)
			}
		}
		for _, name := range tc.rejected {
			if AcceptsRaw(tc.platform, name) {
				t.Errorf("%s accepted %s", tc.platform, name)
			}
		}
	}
}
