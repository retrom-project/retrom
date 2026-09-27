// Package payloadpolicy owns the domain eligibility rules for payload release.
package payloadpolicy

func ReleaseReady(state string, retryable bool, importItemID string) bool {
	switch state {
	case "PUBLISHED", "REVIEW_DISCARDED", "SKIPPED_EXISTING", "SKIPPED_MAPPING",
		"BLOCKED_SOURCE", "BLOCKED_CONTENT", "CANCELLED":
		return true
	case "SOURCE_CHANGED", "READ_FAILED", "COMMIT_FAILED":
		return !retryable
	case "REVIEW_PENDING":
		return importItemID != ""
	default:
		return false
	}
}
