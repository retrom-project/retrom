package cleanupjobs

// CheckReleaseOwner verifies the frozen job and owner identity. Terminal states
// and permitted version advancement are decided by the owning domain.
func CheckReleaseOwner(unit Execution, before EffectOwner, allowAdvanced bool) error {
	owner := before.Owner
	if !before.Found || owner.Scope != unit.Work.Scope || owner.ReleaseJobID != unit.Work.ID ||
		(owner.PayloadState != "RELEASING" && owner.PayloadState != "RELEASED") {
		return Failure("OWNER_CLEANUP_SCOPE_NOT_TERMINAL", nil)
	}
	if owner.Version != unit.Input.Inputs.ScopeVersion && owner.PayloadState != "RELEASED" &&
		(!allowAdvanced || owner.Version <= unit.Input.Inputs.ScopeVersion) {
		return ErrEffectConflict
	}
	return nil
}
