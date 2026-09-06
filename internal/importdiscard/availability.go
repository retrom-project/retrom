package importdiscard

import (
	"context"
	"fmt"
)

func (service *Service) hasUndecidedContent(ctx context.Context, kind, id string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM import_jobs job WHERE job.id=? AND (
 job.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED') OR
 job.rejected_file_count>job.resolved_rejected_file_count AND job.payload_state<>'RELEASED' OR
 EXISTS(SELECT 1 FROM import_items item WHERE item.import_job_id=job.id
 AND item.state NOT IN ('PUBLISHED','DISCARDED'))))`
	if kind != "IMPORT" {
		table, err := batchTable(kind)
		if err != nil {
			return false, err
		}
		query = `SELECT EXISTS(SELECT 1 FROM ` + table[:len(table)-1] + `_items WHERE import_id=?
 AND execution_state NOT IN ('PUBLISHED','REVIEW_DISCARDED','SKIPPED_EXISTING'))`
	}
	var available bool
	if err := service.database.QueryRowContext(ctx, query, id).Scan(&available); err != nil {
		return false, fmt.Errorf("importdiscard/check remaining content: %w", err)
	}
	return available, nil
}
