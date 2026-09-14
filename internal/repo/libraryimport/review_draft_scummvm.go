package libraryimport

import "retrom/internal/model/libraryimport"

func (run *draftPatchRun) applyScummVMSelection() error {
	if run.patch.ScummVMCandidateID == nil {
		return nil
	}
	if run.repository.selectScummVM == nil {
		return libraryimport.ErrInvalid
	}
	validationID, err := run.repository.selectScummVM(
		run.ctx, run.transaction, run.itemID, run.targetID, run.dosEntry, *run.patch.ScummVMCandidateID,
	)
	if err != nil {
		return err
	}
	run.validationID = validationID
	return nil
}
