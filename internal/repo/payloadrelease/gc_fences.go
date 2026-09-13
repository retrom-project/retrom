package payloadrelease

import (
	"context"
	"fmt"

	application "retrom/internal/service/payloadrelease"
)

func (records gcRecords) Fence(ctx context.Context, facts []application.GCBlob) error {
	for start := 0; start < len(facts); start += 200 {
		batch := facts[start:min(start+200, len(facts))]
		ids := make([]string, len(batch))
		expected := make(map[string]application.GCBlob, len(batch))
		for index, blob := range batch {
			ids[index] = blob.ID
			expected[blob.ID] = blob
		}
		marks, args := gcIDParameters(ids)
		result, err := records.executor.ExecContext(ctx, `UPDATE blobs SET id=id WHERE id IN (`+marks+`)`, args...)
		if err := gcWriteCount(result, err, int64(len(batch))); err != nil {
			return fmt.Errorf("lock GC candidate facts: %w", err)
		}
		current, err := records.Selected(ctx, ids)
		if err != nil {
			return err
		}
		if len(current) != len(expected) {
			return application.ErrGCSnapshotChanged
		}
		for _, blob := range current {
			if before, found := expected[blob.ID]; !found || blob != before {
				return application.ErrGCSnapshotChanged
			}
		}
	}
	return nil
}

const gcCandidateFence = ` WHERE blob_id=? AND gc_job_id=? AND first_unreferenced_at_ms=?
 AND scheduled_at_ms=? AND attempt_count=?`

func gcCandidateArguments(blob application.GCBlob) []any {
	return []any{
		blob.ID, blob.Candidate.Work.ID, blob.Candidate.FirstUnreferencedMS,
		blob.Candidate.ScheduledMS, blob.Candidate.Attempt,
	}
}

func (records gcRecords) fenceJob(ctx context.Context, blob application.GCBlob) error {
	if err := workerRecords(records).Fence(ctx, blob.Candidate.Work); err != nil {
		return fmt.Errorf("fence GC job: %w", err)
	}
	return nil
}
