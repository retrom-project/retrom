package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/persistence/releaseschedule"
	"retrom/internal/service/cleanupjobs"
	libraryservice "retrom/internal/service/libraryimport"
)

func (records attachmentRecoveryRecords) CreateJob(ctx context.Context, job cleanupjobs.ScheduledJob) error {
	if err := (releaseschedule.Records{Executor: records.executor}).CreateJob(ctx, job); err != nil {
		return fmt.Errorf("schedule attachment cleanup: %w", err)
	}
	return nil
}

func (records attachmentRecoveryRecords) FinishedInputs(
	ctx context.Context,
) ([]libraryservice.AttachmentInputRelease, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT consumption.id,consumption.version FROM upload_consumptions consumption
JOIN (
 SELECT id,'REVIEW_ARCADE_PARENT' kind FROM review_arcade_parent_attachments WHERE state<>'PENDING'
 UNION ALL SELECT id,'REVIEW_MULTI_DISC' kind FROM review_multidisc_attachments WHERE state<>'PENDING'
) attachment ON attachment.id=consumption.consumer_id AND attachment.kind=consumption.consumer_type
WHERE consumption.released_at_ms IS NULL AND NOT EXISTS(
 SELECT 1 FROM jobs WHERE kind='OWNER_CLEANUP' AND scope_type='UPLOAD_CONSUMPTION' AND scope_id=consumption.id)
ORDER BY consumption.id LIMIT 32`)
	if err != nil {
		return nil, fmt.Errorf("read terminal attachment consumptions: %w", err)
	}
	defer func() { cleanup.Error("close attachment consumptions", rows.Close()) }()
	result := []libraryservice.AttachmentInputRelease{}
	for rows.Next() {
		var input libraryservice.AttachmentInputRelease
		if err := rows.Scan(&input.ID, &input.Version); err != nil {
			return nil, fmt.Errorf("scan attachment consumption: %w", err)
		}
		result = append(result, input)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachment consumptions: %w", err)
	}
	return result, nil
}
