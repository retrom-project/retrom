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
	if !terminalEffectOwner(owner) || owner.ReleaseJobID != unit.Work.ID || owner.PublicID != "" {
		return effectFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL", nil)
	}
	retryGame := owner.Scope.Type == ScopeGame && owner.PayloadState == "FAILED"
	if owner.Version != unit.Input.Inputs.ScopeVersion && owner.PayloadState != "RELEASED" && !retryGame {
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
	case ScopePegasusImportItem, ScopeEmulationStationImportItem:
		return TerminalSourceItem(owner.State, owner.Retryable)
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
		file.ActiveConsumptions == 0 && file.DomainReferences == 0
}

func effectReason(owner Owner) Reason {
	switch owner.Scope.Type {
	case ScopeGame:
		return ReasonGameDeleted
	case ScopeImportJob:
		return ReasonImportTerminal
	case ScopePegasusImportItem:
		return ReasonPegasusTerminal
	case ScopeEmulationStationImportItem:
		return ReasonEmulationStationTerminal
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
	return owner.PayloadState == "RELEASING" || owner.PayloadState == "FAILED" || owner.PayloadState == "RELEASED"
}

func validEffectChild(parent, child EffectOwner) bool {
	return child.Found && child.ParentID == parent.Owner.Scope.ID && terminalEffectOwner(
		child.Owner,
	) && releasingEffectOwner(
		child.Owner,
	) && child.Owner.ReleaseJobID != ""
}

func gameEffectSources(owner EffectOwner) []Scope {
	links := make([]Scope, 0, 2)
	for _, source := range []EffectSource{owner.MetadataSource, owner.ContentSource} {
		link, found := gameEffectSource(source)
		if found && (len(links) == 0 || links[0] != link) {
			links = append(links, link)
		}
	}
	return links
}

func gameEffectSource(source EffectSource) (Scope, bool) {
	var scope ScopeType
	switch source.Kind {
	case "IMPORT_REVIEW":
		scope = ScopeImportItem
	case "SERVER_PEGASUS_IMPORT":
		scope = ScopePegasusImportItem
	case "SERVER_EMULATIONSTATION_IMPORT":
		scope = ScopeEmulationStationImportItem
	default:
		return Scope{}, false
	}
	return Scope{Type: scope, ID: source.ID}, source.ID != ""
}
