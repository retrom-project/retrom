package payloadrelease

import model "retrom/internal/model/payloadrelease"

func validateEffectRoot(unit model.Execution, facts model.EffectOwner) error {
	owner := facts.Owner
	if !facts.Found || owner.Scope != unit.Work.Scope {
		return effectFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL", nil)
	}
	if owner.Scope.Type == model.ScopeUploadConsumption {
		if owner.Version != unit.Input.Inputs.ScopeVersion && !facts.Consumption.Released.Set {
			return effectFailure("PAYLOAD_RELEASE_SCOPE_VERSION_MISMATCH", nil)
		}
		return nil
	}
	if !terminalEffectOwner(owner) || owner.ReleaseJobID != unit.Work.ID || owner.PublicID != "" {
		return effectFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL", nil)
	}
	retryGame := owner.Scope.Type == model.ScopeGame && owner.PayloadState == "FAILED"
	if owner.Version != unit.Input.Inputs.ScopeVersion && owner.PayloadState != "RELEASED" && !retryGame {
		return effectFailure("PAYLOAD_RELEASE_SCOPE_VERSION_MISMATCH", nil)
	}
	if !releasingEffectOwner(owner) {
		return effectFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL", nil)
	}
	return nil
}

func terminalEffectOwner(owner model.Owner) bool {
	switch owner.Scope.Type {
	case model.ScopeGame:
		return owner.State == "DELETED"
	case model.ScopeImportItem:
		return model.TerminalImportItem(owner.State)
	case model.ScopeImportJob:
		return model.TerminalImportJob(owner.State)
	case model.ScopePegasusImportItem, model.ScopeEmulationStationImportItem:
		return model.TerminalSourceItem(owner.State, owner.Retryable)
	case model.ScopeUploadConsumption, model.ScopeBlob:
		return false
	default:
		return false
	}
}

func eligibleEffectUpload(file model.EffectUpload) bool {
	terminal := file.SessionState == "COMPLETE" || file.SessionState == "FAILED" ||
		file.SessionState == "CANCELLED" || file.SessionState == "EXPIRED"
	return file.ID != "" && file.BlobID != "" && file.State == "COMPLETE" && terminal &&
		file.ActiveConsumptions == 0 && file.DomainReferences == 0
}

func effectReason(owner model.Owner) model.Reason {
	switch owner.Scope.Type {
	case model.ScopeGame:
		return model.ReasonGameDeleted
	case model.ScopeImportJob:
		return model.ReasonImportTerminal
	case model.ScopePegasusImportItem:
		return model.ReasonPegasusTerminal
	case model.ScopeEmulationStationImportItem:
		return model.ReasonEmulationStationTerminal
	case model.ScopeImportItem:
		switch owner.State {
		case "PUBLISHED":
			return model.ReasonImportPublished
		case "DISCARDED":
			return model.ReasonImportDiscarded
		case "CANCELLED":
			return model.ReasonImportCancelled
		default:
			return model.ReasonImportFailed
		}
	case model.ScopeUploadConsumption, model.ScopeBlob:
		return model.ReasonUploadConsumed
	default:
		return model.ReasonUploadConsumed
	}
}

func releasingEffectOwner(owner model.Owner) bool {
	return owner.PayloadState == "RELEASING" || owner.PayloadState == "FAILED" || owner.PayloadState == "RELEASED"
}

func validEffectChild(parent, child model.EffectOwner) bool {
	return child.Found && child.ParentID == parent.Owner.Scope.ID && terminalEffectOwner(
		child.Owner,
	) && releasingEffectOwner(
		child.Owner,
	) && child.Owner.ReleaseJobID != ""
}

func gameEffectSources(owner model.EffectOwner) []model.Scope {
	links := make([]model.Scope, 0, 2)
	for _, source := range []model.EffectSource{owner.MetadataSource, owner.ContentSource} {
		link, found := gameEffectSource(source)
		if found && (len(links) == 0 || links[0] != link) {
			links = append(links, link)
		}
	}
	return links
}

func gameEffectSource(source model.EffectSource) (model.Scope, bool) {
	var scope model.ScopeType
	switch source.Kind {
	case "IMPORT_REVIEW":
		scope = model.ScopeImportItem
	case "SERVER_PEGASUS_IMPORT":
		scope = model.ScopePegasusImportItem
	case "SERVER_EMULATIONSTATION_IMPORT":
		scope = model.ScopeEmulationStationImportItem
	default:
		return model.Scope{}, false
	}
	return model.Scope{Type: scope, ID: source.ID}, source.ID != ""
}
