// Package payloadpolicy owns the domain eligibility rules for payload release.
package payloadpolicy

func ItemTerminal(state string) bool {
	switch state {
	case "PUBLISHED", "DISCARDED", "FAILED_FINAL", "CANCELLED":
		return true
	default:
		return false
	}
}

func JobTerminal(state string) bool {
	return state == "COMPLETED" || state == "CANCELLED" || state == "FAILED"
}
