package filedeletion

import (
	"context"
	"fmt"

	application "retrom/internal/service/cleanupjobs"
)

func (records deletionRecords) Fence(ctx context.Context, facts []application.DeletionFile) error {
	for start := 0; start < len(facts); start += 200 {
		batch := facts[start:min(start+200, len(facts))]
		ids := make([]string, len(batch))
		expected := make(map[string]application.DeletionFile, len(batch))
		for index, blob := range batch {
			ids[index] = blob.ID
			expected[blob.ID] = blob
		}
		marks, args := deletionIDParameters(ids)
		result, err := records.executor.ExecContext(
			ctx,
			`UPDATE stored_files SET id=id WHERE id IN (`+marks+`)`,
			args...)
		if err := deletionWriteCount(result, err, int64(len(batch))); err != nil {
			return fmt.Errorf("lock file deletion candidate facts: %w", err)
		}
		current, err := records.Selected(ctx, ids)
		if err != nil {
			return err
		}
		if len(current) != len(expected) {
			return application.ErrDeletionSnapshotChanged
		}
		for _, blob := range current {
			if before, found := expected[blob.ID]; !found || blob != before {
				return application.ErrDeletionSnapshotChanged
			}
		}
	}
	return nil
}

const deletionCandidateFence = ` WHERE blob_id=? AND deletion_job_id=? AND retired_at_ms=?
 AND scheduled_at_ms=? AND attempt_count=?`

func deletionCandidateArguments(blob application.DeletionFile) []any {
	return []any{
		blob.ID, blob.Candidate.Work.ID, blob.Candidate.RetiredMS,
		blob.Candidate.ScheduledMS, blob.Candidate.Attempt,
	}
}

func (records deletionRecords) fenceJob(ctx context.Context, blob application.DeletionFile) error {
	if err := records.worker.Write.Fence(ctx, blob.Candidate.Work); err != nil {
		return fmt.Errorf("fence file deletion job: %w", err)
	}
	return nil
}
