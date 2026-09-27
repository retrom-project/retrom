// Package payloadpolicy owns the domain eligibility rules for payload release.
package payloadpolicy

func CanPurge(state, sessionState, id, blobID string, activeConsumptions int64) bool {
	terminal := sessionState == "COMPLETE" || sessionState == "FAILED" ||
		sessionState == "CANCELLED" || sessionState == "EXPIRED"
	return id != "" && blobID != "" && state == "COMPLETE" && terminal && activeConsumptions == 0
}
