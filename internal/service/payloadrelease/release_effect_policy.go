package payloadrelease

import (
	gamepolicy "retrom/internal/service/gamecontent/payloadpolicy"
	importpolicy "retrom/internal/service/libraryimport/payloadpolicy"
	sourcepolicy "retrom/internal/service/sourceimport/payloadpolicy"
	uploadpolicy "retrom/internal/service/uploads/payloadpolicy"
)

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
		return gamepolicy.ReleaseReady(owner.State)
	case ScopeImportItem:
		return importpolicy.ItemTerminal(owner.State)
	case ScopeImportJob:
		return importpolicy.JobTerminal(owner.State)
	case ScopeSourceImportItem:
		return sourcepolicy.ReleaseReady(owner.State, owner.Retryable, owner.PublicID)
	case ScopeUploadConsumption, ScopeBlob:
		return false
	default:
		return false
	}
}

func eligibleEffectUpload(file EffectUpload) bool {
	return uploadpolicy.CanPurge(file.State, file.SessionState, file.ID, file.BlobID, file.ActiveConsumptions)
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
