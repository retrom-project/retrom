package libraryimport

import (
	"encoding/json"

	"retrom/internal/scummvm"
)

func (run *draftPatchRun) applyScummVMSelection() error {
	if run.patch.ScummVMCandidateID == nil {
		return nil
	}
	state := draftValidationRefresh{
		service: run.service, ctx: run.ctx, transaction: run.transaction,
		itemID: run.itemID, targetID: run.targetID, dosEntry: run.dosEntry,
	}
	if err := state.loadInputs(); err != nil {
		return err
	}
	if state.contentKind != scummvm.ContentKind || state.providerID != "retrom-runtime" ||
		state.runtimeTargetID != "scummvm" {
		return ErrInvalid
	}
	_, current, err := state.loadExactValidation()
	if err != nil {
		return err
	}
	if !current || state.sourceID == "" {
		return ErrInvalid
	}
	snapshot, err := scummvm.ParseSnapshot(state.dependencySnapshot)
	if err != nil || snapshot.Detection.SourceDigest != state.effectiveManifestDigest {
		return ErrInvalid
	}
	selected, err := snapshot.Select(*run.patch.ScummVMCandidateID)
	if err != nil {
		return ErrInvalid
	}
	if selected.SelectedCandidateID == snapshot.SelectedCandidateID {
		run.validationID = state.sourceID
		return nil
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		return ErrInvalid
	}
	state.dependencySnapshot = string(encoded)
	state.sourceStatus, state.compatibilityCode = selected.Status()
	run.validationID, err = state.insertValidation()
	return err
}

func (state *draftValidationRefresh) resolveScummVMSelection() (draftDependencyState, error) {
	snapshot, err := scummvm.ParseSnapshot(state.dependencySnapshot)
	if err != nil || snapshot.Detection.SourceDigest != state.effectiveManifestDigest {
		return draftDependencyState{}, ErrInvalid
	}
	status, code := snapshot.Status()
	return draftDependencyState{tracked: true, status: status, code: code, snapshotJSON: state.dependencySnapshot}, nil
}

func (run *approvalRun) prepareScummVMSelection() error {
	snapshot, err := scummvm.ParseSnapshot(run.dependencySnapshotJSON)
	if err != nil || run.validationStatus != "READY" || snapshot.Detection.SourceDigest != run.sourceManifestDigest {
		return ErrInvalid
	}
	if _, err := snapshot.Selected(); err != nil {
		return ErrInvalid
	}
	run.runtimeDependencySnapshotJSON = run.dependencySnapshotJSON
	return nil
}
