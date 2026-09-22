package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	payloadpersistence "retrom/internal/persistence/payloadrelease"

	"retrom/internal/dbexec"
	tagpersistence "retrom/internal/persistence/tagging"
	application "retrom/internal/service/libraryimport"
)

type (
	ReviewDiscards       struct{ database *sql.DB }
	reviewDiscardRecords struct{ executor dbexec.Executor }
)

func NewReviewDiscards(database *sql.DB) *ReviewDiscards { return &ReviewDiscards{database: database} }

func (repository *ReviewDiscards) WithDiscard(
	ctx context.Context, work func(application.ReviewDiscardScope) error,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin review discard: %w", err)
	}
	defer dbexec.Rollback(transaction)
	if err := work(BindReviewDiscard(transaction)); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit review discard: %w", err)
	}
	return nil
}

// BindReviewDiscard joins an existing transaction without committing it.
func BindReviewDiscard(executor dbexec.Executor) application.ReviewDiscardScope {
	records := reviewDiscardRecords{executor: executor}
	return application.ReviewDiscardScope{
		Payload: payloadpersistence.BindReleases(executor),
		Reader:  records, Writer: records, Tags: tagpersistence.BindExecutor(executor).Relations,
	}
}

func (records reviewDiscardRecords) Snapshot(
	ctx context.Context, itemID string,
) (application.ReviewDiscardSnapshot, bool, error) {
	var result application.ReviewDiscardSnapshot
	err := records.executor.QueryRowContext(ctx, `
SELECT d.id,i.import_job_id,d.metadata_json,d.version,i.state,
d.selected_validation_id,v.dat_version_id,d.selected_candidate_id,
(d.cover_candidate_asset_id IS NOT NULL OR d.cover_uploaded_asset_id IS NOT NULL),
d.background_candidate_asset_id IS NOT NULL,
(EXISTS(SELECT 1 FROM source_import_items source
 WHERE source.library_import_item_id=i.id AND source.execution_state<>'REVIEW_PENDING')),
j.version,j.state,j.queued_item_count,j.running_item_count,j.review_pending_item_count,
j.failed_item_count,j.cancelled_item_count,j.rejected_file_count,j.resolved_rejected_file_count,
j.cancel_requested_at_ms,j.completed_at_ms
FROM import_items i
JOIN import_jobs j ON j.id=i.import_job_id
JOIN review_drafts d ON d.import_item_id=i.id
LEFT JOIN import_item_core_validations v ON v.id=d.selected_validation_id
WHERE i.id=?`, itemID).Scan(&result.DraftID, &result.ImportID, &result.MetadataJSON, &result.Version,
		&result.State, &result.ValidationID, &result.DatID, &result.CandidateID,
		&result.HasCover, &result.HasBackground, &result.SourceBusy,
		&result.Aggregate.Version, &result.Aggregate.Progress.State,
		&result.Aggregate.Progress.Counts.Queued, &result.Aggregate.Progress.Counts.Running,
		&result.Aggregate.Progress.Counts.ReviewPending, &result.Aggregate.Progress.Counts.Failed,
		&result.Aggregate.Progress.Counts.Cancelled, &result.Aggregate.Progress.Counts.Rejected,
		&result.Aggregate.Progress.Counts.ResolvedRejected,
		&result.Aggregate.Progress.CancelRequestedAtMS, &result.Aggregate.Progress.CompletedAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ReviewDiscardSnapshot{}, false, nil
	}
	if err != nil {
		return application.ReviewDiscardSnapshot{}, false, fmt.Errorf("query discard snapshot: %w", err)
	}
	return result, true, nil
}

func (records reviewDiscardRecords) TransitionOwner(
	ctx context.Context, change application.ReviewOwnerTransition,
) error {
	return TransitionReviewOwners(ctx, records.executor, change)
}
