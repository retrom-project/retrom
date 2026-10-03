package libraryimport

import "testing"

func TestReviewScreenshotAuthorizesEveryContentKind(t *testing.T) {
	for _, kind := range []string{"SINGLE_FILE", "SCUMMVM_PROJECT", "RPGMAKER_PROJECT", "ONS_PROJECT"} {
		for _, ready := range []bool{false, true} {
			if !ReviewApproval(ready, true) {
				t.Fatalf("screenshot failed to authorize %s ready=%v", kind, ready)
			}
			if ReviewApproval(ready, false) != ready {
				t.Fatalf("unscreened %s did not follow readiness", kind)
			}
		}
	}
}
