//go:build integration

package launch

import "testing"

func TestPlatformExpansionReviewPublishesProductLaunch(t *testing.T) {
	// Owned inert bytes exercise import/preview/publish/launch contracts only.
	// Executable game compatibility is covered separately by ACC-RUN-016.
	cases := []singleBlobCase{
		{"gamegear", "genesis_plus_gx", "genesis-plus-gx", "ROM_BLOB", "Owned.gg", make([]byte, 32768)},
		{"sg1000", "genesis_plus_gx", "genesis-plus-gx", "ROM_BLOB", "Owned.sg", make([]byte, 32768)},
		{"multivision", "genesis_plus_gx", "genesis-plus-gx", "ROM_BLOB", "Owned.sg", make([]byte, 32768)},
		{"pico", "picodrive", "picodrive", "ROM_BLOB", "Owned.bin", make([]byte, 32768)},
		{"sega32x", "picodrive", "picodrive", "ROM_BLOB", "Owned.32x", make([]byte, 32768)},
		{"supergrafx", "mednafen_pce", "mednafen-pce", "ROM_BLOB", "Owned.sgx", make([]byte, 32768)},
		{"gx4000", "cap32", "cap32", "ROM_BLOB", "Owned.cpr", make([]byte, 32768)},
	}
	for _, input := range cases {
		t.Run(input.platform, func(t *testing.T) { verifySingleBlobReview(t, input) })
	}
}
