//go:build integration

package launch

import "testing"

func TestMSXReviewPublishesProductLaunch(t *testing.T) {
	// Owned cartridge header. This protocol test does not claim emulator compatibility.
	fixture := make([]byte, 16384)
	copy(fixture, []byte{'A', 'B', 16, 64})
	verifySingleBlobReview(t, singleBlobCase{
		platform: "msx", core: "webmsx", target: "msx-webmsx", kind: "ROM_BLOB", filename: "Owned.rom", bytes: fixture,
	})
}
