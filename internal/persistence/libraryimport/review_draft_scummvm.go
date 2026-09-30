package libraryimport

import (
	"fmt"

	"retrom/internal/core/scummvm"
	"retrom/internal/profilemodel"
	service "retrom/internal/service/libraryimport"
)

func (run *draftPatchRun) applyScummVMSelection() error {
	if run.patch.ScummVMCandidateID == nil {
		return nil
	}
	runtime, err := ReadReviewRuntime(run.ctx, run.transaction, run.itemID)
	if err != nil {
		return fmt.Errorf("apply ScummVM choice: %w", err)
	}
	snapshot, err := scummvm.ParseSnapshot(runtime.DependencyJSON)
	if err != nil || snapshot.Detection.SourceDigest != runtime.ManifestDigest {
		return service.ErrInvalid
	}
	selected, err := snapshot.Select(*run.patch.ScummVMCandidateID)
	if err != nil {
		return service.ErrInvalid
	}
	encoded, err := profilemodel.Encode(profilemodel.Review, profilemodel.ScummVMProject, &selected)
	if err != nil {
		return fmt.Errorf("apply ScummVM choice: %w", err)
	}
	if _, err := run.transaction.ExecContext(run.ctx, `
UPDATE import_items SET review_profile_json=? WHERE id=?`, encoded, run.itemID); err != nil {
		return fmt.Errorf("save ScummVM choice: %w", err)
	}
	return nil
}
