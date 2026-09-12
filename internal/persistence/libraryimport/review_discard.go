package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/persistence/recordstore"
	tagpersistence "retrom/internal/persistence/tagging"
	application "retrom/internal/service/libraryimport"
)

type (
	ReviewDiscards       struct{ database *sql.DB }
	reviewDiscardRecords struct{ transaction *sql.Tx }
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

// BindReviewDiscard joins an existing real transaction without committing it.
func BindReviewDiscard(transaction *sql.Tx) application.ReviewDiscardScope {
	records := reviewDiscardRecords{transaction: transaction}
	return application.ReviewDiscardScope{
		Reader: records, Writer: records, Tags: tagpersistence.Bind(transaction).Relations,
	}
}

func (records reviewDiscardRecords) Snapshot(
	ctx context.Context, itemID string,
) (application.ReviewDiscardSnapshot, bool, error) {
	var result application.ReviewDiscardSnapshot
	err := records.transaction.QueryRowContext(ctx, `
SELECT d.id,i.import_job_id,d.metadata_json,d.version,i.state,i.review_handoff_kind,
d.selected_validation_id,v.dat_version_id,d.selected_candidate_id,
(d.cover_candidate_asset_id IS NOT NULL OR d.cover_uploaded_asset_id IS NOT NULL),
d.background_candidate_asset_id IS NOT NULL,
EXISTS(SELECT 1 FROM emulationstation_import_items source
 WHERE source.library_import_item_id=i.id AND source.execution_state='REVIEW_PENDING'),
(EXISTS(SELECT 1 FROM pegasus_import_items source
 WHERE source.library_import_item_id=i.id AND source.execution_state<>'REVIEW_PENDING') OR
 EXISTS(SELECT 1 FROM emulationstation_import_items source
 WHERE source.library_import_item_id=i.id AND source.execution_state<>'REVIEW_PENDING')),
j.version,j.state,j.queued_item_count,j.running_item_count,j.review_pending_item_count,
j.failed_item_count,j.cancelled_item_count,j.rejected_file_count,j.resolved_rejected_file_count,
j.cancel_requested_at_ms,j.completed_at_ms
FROM import_items i
JOIN import_jobs j ON j.id=i.import_job_id
JOIN review_drafts d ON d.import_item_id=i.id
LEFT JOIN import_item_core_validations v ON v.id=d.selected_validation_id
WHERE i.id=?`, itemID).Scan(&result.DraftID, &result.ImportID, &result.MetadataJSON, &result.Version,
		&result.State, &result.HandoffKind, &result.ValidationID, &result.DatID, &result.CandidateID,
		&result.HasCover, &result.HasBackground, &result.EmulationStationReady, &result.SourceBusy,
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

func (records reviewDiscardRecords) RecordEvent(ctx context.Context, event application.ReviewDiscardEvent) error {
	var reason *string
	if event.Reason != "" {
		reason = &event.Reason
	}
	_, err := recordstore.CreateReviewEvents(ctx, records.transaction, `
INSERT INTO review_events(
 id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,before_json,
 after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,reason,created_at_ms
) VALUES(?,?,'DISCARDED',?,?,?,?,
 '{"schemaVersion":2,"decision":"DISCARDED"}',
 '{"schemaVersion":2,"decision":"DISCARDED"}',?,?,?,?,?)
`, event.ID, event.ItemID, event.ActorKind, event.ActorUserID, event.ActorLabel, event.BeforeJSON,
		event.ConfigJSON, event.DatJSON, event.ProviderJSON, reason, event.NowMS)
	if err != nil {
		return fmt.Errorf("insert discard event: %w", err)
	}
	return nil
}

func (records reviewDiscardRecords) TransitionOwner(
	ctx context.Context, change application.ReviewOwnerTransition,
) error {
	return TransitionReviewOwners(ctx, records.transaction, change)
}

func (records reviewDiscardRecords) SchedulePayload(
	ctx context.Context, change application.ReviewPayloadRelease,
) error {
	return ScheduleReviewPayloads(ctx, records.transaction, change)
}
