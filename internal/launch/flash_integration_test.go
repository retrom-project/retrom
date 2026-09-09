//go:build integration

package launch

import "testing"

func TestFlashReviewPublishesProductLaunch(t *testing.T) {
	// Owned minimal uncompressed SWF: empty 1x1 stage, one frame, no ActionScript.
	fixture := []byte{'F', 'W', 'S', 9, 18, 0, 0, 0, 8, 0, 0, 30, 1, 0, 64, 0, 0, 0}
	verifySingleBlobReview(t, singleBlobCase{
		platform: "flash", target: "flash-ruffle", kind: "ROM_BLOB", filename: "Owned.swf", bytes: fixture,
	})
}
