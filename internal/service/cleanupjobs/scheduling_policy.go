package cleanupjobs

func validScheduleScope(value ScopeType) bool {
	switch value {
	case ScopeImportItem, ScopeImportJob, ScopeSourceImportItem,
		ScopeUploadConsumption, ScopeGame:
		return true
	case ScopeFile:
		return false
	default:
		return false
	}
}

func validReason(value Reason) bool {
	switch value {
	case ReasonImportPublished, ReasonImportDiscarded, ReasonImportFailed, ReasonImportCancelled,
		ReasonImportTerminal, ReasonSourceTerminal,
		ReasonUploadConsumed, ReasonGameDeleted:
		return true
	default:
		return false
	}
}
