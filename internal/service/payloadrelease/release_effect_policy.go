package payloadrelease

func validateEffectRoot(unit Execution, facts EffectOwner) error {
	owner := facts.Owner
	if !facts.Found || owner.Scope != unit.Work.Scope {
		return effectFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL", nil)
	}
	if owner.Scope.Type == ScopeUploadConsumption {
		if owner.Version != unit.Input.Inputs.ScopeVersion && !facts.Consumption.Released.Set {
			return effectFailure("PAYLOAD_RELEASE_SCOPE_VERSION_MISMATCH", nil)
		}
		return nil
	}
	if !terminalEffectOwner(owner) || owner.ReleaseJobID != unit.Work.ID {
		return effectFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL", nil)
	}
	if owner.Version != unit.Input.Inputs.ScopeVersion && owner.PayloadState != "RELEASED" &&
		(owner.Scope.Type != ScopeSourceImportItem || owner.Version <= unit.Input.Inputs.ScopeVersion) {
		return effectFailure("PAYLOAD_RELEASE_SCOPE_VERSION_MISMATCH", nil)
	}
	if !releasingEffectOwner(owner) {
		return effectFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL", nil)
	}
	return nil
}

func terminalEffectOwner(owner Owner) bool {
	switch owner.Scope.Type {
	case ScopeGame:
		return owner.State == "DELETED"
	case ScopeImportItem:
		return TerminalImportItem(owner.State)
	case ScopeImportJob:
		return TerminalImportJob(owner.State)
	case ScopeSourceImportItem:
		return releasableSource(owner)
	case ScopeUploadConsumption, ScopeBlob:
		return false
	default:
		return false
	}
}

func eligibleEffectUpload(file EffectUpload) bool {
	terminal := file.SessionState == "COMPLETE" || file.SessionState == "FAILED" ||
		file.SessionState == "CANCELLED" || file.SessionState == "EXPIRED"
	return file.ID != "" && file.BlobID != "" && file.State == "COMPLETE" && terminal &&
		file.ActiveConsumptions == 0
}

func effectReason(owner Owner) Reason {
	switch owner.Scope.Type {
	case ScopeGame:
		return ReasonGameDeleted
	case ScopeImportJob:
		return ReasonImportTerminal
	case ScopeSourceImportItem:
		return ReasonSourceTerminal

	case ScopeImportItem:
		switch owner.State {
		case "PUBLISHED":
			return ReasonImportPublished
		case "DISCARDED":
			return ReasonImportDiscarded
		case "CANCELLED":
			return ReasonImportCancelled
		default:
			return ReasonImportFailed
		}
	case ScopeUploadConsumption, ScopeBlob:
		return ReasonUploadConsumed
	default:
		return ReasonUploadConsumed
	}
}

func releasingEffectOwner(owner Owner) bool {
	return owner.PayloadState == "RELEASING" || owner.PayloadState == "RELEASED"
}

func validEffectChild(parent, child EffectOwner) bool {
	return child.Found && child.ParentID == parent.Owner.Scope.ID && terminalEffectOwner(
		child.Owner,
	) && child.Owner.PayloadState == "RELEASED" && child.Owner.ReleaseJobID != ""
}
