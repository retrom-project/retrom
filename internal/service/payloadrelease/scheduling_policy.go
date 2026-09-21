package payloadrelease

func TerminalImportItem(state string) bool {
	switch state {
	case "PUBLISHED", "DISCARDED", "FAILED_FINAL", "CANCELLED":
		return true
	default:
		return false
	}
}

func TerminalImportJob(state string) bool {
	return state == "COMPLETED" || state == "CANCELLED" || state == "FAILED"
}

func TerminalSourceItem(state string, retryable bool) bool {
	switch state {
	case "PUBLISHED", "REVIEW_DISCARDED", "SKIPPED_EXISTING", "SKIPPED_MAPPING",
		"BLOCKED_SOURCE", "BLOCKED_CONTENT", "CANCELLED":
		return true
	case "SOURCE_CHANGED", "READ_FAILED", "COMMIT_FAILED":
		return !retryable
	default:
		return false
	}
}

func validScheduleScope(value ScopeType) bool {
	switch value {
	case ScopeImportItem, ScopeImportJob, ScopePegasusImportItem,
		ScopeEmulationStationImportItem, ScopeUploadConsumption, ScopeGame:
		return true
	case ScopeBlob:
		return false
	default:
		return false
	}
}

func validReason(value Reason) bool {
	switch value {
	case ReasonImportPublished, ReasonImportDiscarded, ReasonImportFailed, ReasonImportCancelled,
		ReasonImportTerminal, ReasonPegasusTerminal, ReasonEmulationStationTerminal,
		ReasonUploadConsumed, ReasonGameDeleted:
		return true
	default:
		return false
	}
}
