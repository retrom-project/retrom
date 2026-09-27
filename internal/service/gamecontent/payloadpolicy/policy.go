// Package payloadpolicy owns the domain eligibility rules for payload release.
package payloadpolicy

import "math"

func CanDelete(state, payloadState string, version, expected int64) bool {
	return version == expected && version < math.MaxInt64 && state == "PUBLISHED" && payloadState == "RETAINED"
}
func ReleaseReady(state string) bool { return state == "DELETED" }
