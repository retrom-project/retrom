package payloadrelease

import application "retrom/internal/service/payloadrelease"

func validEffectRemoval(group application.EffectReferenceGroup, scope application.ScopeType) bool {
	switch group {
	case application.EffectGameRuntime, application.EffectGameEvidence, application.EffectGameFiles:
		return scope == application.ScopeGame
	case application.EffectImportReview, application.EffectImportEvidence, application.EffectImportFiles:
		return scope == application.ScopeImportItem
	case application.EffectSourceFiles, application.EffectSourceAssets:
		return scope == application.ScopePegasusImportItem || scope == application.ScopeEmulationStationImportItem
	default:
		return false
	}
}
