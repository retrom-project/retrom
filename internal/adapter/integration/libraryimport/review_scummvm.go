package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"

	"retrom/internal/capability/engine/scummvm"
	"retrom/internal/persistence/dbexec"
)

func (service *Service) selectScummVMCandidate(
	ctx context.Context,
	transaction dbexec.Executor,
	itemID, targetID string,
	dosEntry sql.NullString,
	candidateID string,
) (string, error) {
	state := draftValidationRefresh{
		service: service, ctx: ctx, transaction: transaction,
		itemID: itemID, targetID: targetID, dosEntry: dosEntry,
	}
	if err := state.loadInputs(); err != nil {
		return "", err
	}
	if state.contentKind != scummvm.ContentKind || state.providerID != "retrom-runtime" ||
		state.runtimeTargetID != "scummvm" {
		return "", ErrInvalid
	}
	_, current, err := state.loadExactValidation()
	if err != nil {
		return "", err
	}
	if !current || state.sourceID == "" {
		return "", ErrInvalid
	}
	snapshot, err := scummvm.ParseSnapshot(state.dependencySnapshot)
	if err != nil || snapshot.Detection.SourceDigest != state.effectiveManifestDigest {
		return "", ErrInvalid
	}
	selected, err := snapshot.Select(candidateID)
	if err != nil {
		return "", ErrInvalid
	}
	if selected.SelectedCandidateID == snapshot.SelectedCandidateID {
		return state.sourceID, nil
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		return "", ErrInvalid
	}
	state.dependencySnapshot = string(encoded)
	state.sourceStatus, state.compatibilityCode = selected.Status()
	validationID, err := state.insertValidation()
	return validationID, err
}

func (state *draftValidationRefresh) resolveScummVMSelection() (draftDependencyState, error) {
	snapshot, err := scummvm.ParseSnapshot(state.dependencySnapshot)
	if err != nil || snapshot.Detection.SourceDigest != state.effectiveManifestDigest {
		return draftDependencyState{}, ErrInvalid
	}
	status, code := snapshot.Status()
	return draftDependencyState{tracked: true, status: status, code: code, snapshotJSON: state.dependencySnapshot}, nil
}
