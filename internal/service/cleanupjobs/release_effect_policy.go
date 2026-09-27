package cleanupjobs

import (
	"retrom/internal/cleanup"
	gamepolicy "retrom/internal/service/gamecontent/payloadpolicy"
	importpolicy "retrom/internal/service/libraryimport/payloadpolicy"
	sourcepolicy "retrom/internal/service/sourceimport/payloadpolicy"
	uploadpolicy "retrom/internal/service/uploads/payloadpolicy"
)

func validateEffectRoot(unit Execution, facts EffectOwner) error {
	owner := facts.Owner
	if !facts.Found || owner.Scope != unit.Work.Scope {
		return effectFailure("OWNER_CLEANUP_SCOPE_NOT_TERMINAL", nil)
	}
	if owner.Scope.Type == ScopeUploadConsumption {
		if owner.Version != unit.Input.Inputs.ScopeVersion && !facts.Consumption.Released.Set {
			return effectFailure("OWNER_CLEANUP_SCOPE_VERSION_MISMATCH", nil)
		}
		return nil
	}
	if !terminalEffectOwner(owner) || owner.ReleaseJobID != unit.Work.ID {
		return effectFailure("OWNER_CLEANUP_SCOPE_NOT_TERMINAL", nil)
	}
	plan, err := ownerCleanupPlan(owner.Scope.Type)
	if err != nil {
		return err
	}
	if owner.Version != unit.Input.Inputs.ScopeVersion && owner.PayloadState != "RELEASED" &&
		(!plan.AllowAdvancedVersion || owner.Version <= unit.Input.Inputs.ScopeVersion) {
		return effectFailure("OWNER_CLEANUP_SCOPE_VERSION_MISMATCH", nil)
	}
	if !releasingEffectOwner(owner) {
		return effectFailure("OWNER_CLEANUP_SCOPE_NOT_TERMINAL", nil)
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
	case ScopeUploadConsumption, ScopePath:
		return false
	default:
		return false
	}
}

func eligibleEffectUpload(file EffectUpload) bool {
	return uploadpolicy.CanPurge(file.State, file.SessionState, file.ID, file.FileRecord, file.ActiveConsumptions)
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
	case ScopeUploadConsumption, ScopePath:
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

func ownerCleanupPlan(scope ScopeType) (cleanup.Plan, error) {
	switch scope {
	case ScopeGame:
		return gamepolicy.Cleanup(), nil
	case ScopeImportItem:
		return importpolicy.ItemCleanup(), nil
	case ScopeImportJob:
		return importpolicy.JobCleanup(), nil
	case ScopeSourceImportItem:
		return sourcepolicy.Cleanup(), nil
	case ScopeUploadConsumption, ScopePath:
		return cleanup.Plan{}, ErrScopeInvalid
	default:
		return cleanup.Plan{}, ErrScopeInvalid
	}
}
