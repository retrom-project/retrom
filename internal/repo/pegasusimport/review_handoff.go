package pegasusimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	librarymodel "retrom/internal/model/libraryimport"
	application "retrom/internal/model/pegasusimport"
	"retrom/internal/repo/dbexec"
	libraryrepo "retrom/internal/repo/libraryimport"
	"retrom/internal/repo/recordstore"
)

type ReviewHandoff struct{ database *sql.DB }

func NewReviewHandoff(database *sql.DB) *ReviewHandoff { return &ReviewHandoff{database: database} }

func (repository *ReviewHandoff) CommitReviewHandoff(
	ctx context.Context,
	request application.ReviewHandoffRequest,
	nowMS int64,
	auditID, actorKind string,
	actorUserID, actorLabel *string,
	maximumYear int,
) error {
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := reviewHandoffRecords{executor}
		before, err := records.CurrentReviewHandoff(ctx, request.ItemID)
		if err != nil {
			return fmt.Errorf("read Pegasus review handoff: %w", err)
		}
		if before.Identity != request || request.LibraryItemID == "" || request.LibraryJobID == "" {
			return application.ErrVersionConflict
		}
		if before.State == "REVIEW_PENDING" {
			return nil
		}
		if !application.CanCompleteReviewHandoff(before, nowMS) {
			return application.ErrVersionConflict
		}
		input := librarymodel.MetadataSeedInput{
			ItemID: request.LibraryItemID, Metadata: before.Metadata,
			MaximumYear: maximumYear, NowMS: nowMS,
			AuditID: auditID, ActorKind: actorKind,
			ActorUserID: actorUserID, ActorLabel: actorLabel,
		}
		_, warnings, err := libraryrepo.SeedMetadata(ctx, executor, input)
		if err != nil {
			return fmt.Errorf("seed Pegasus review metadata: %w", err)
		}
		change := application.ReviewHandoffChange{
			Before:   before,
			Warnings: application.MergeReviewMetadataWarnings(before.Warnings, warnings),
			NowMS:    nowMS,
		}
		if err := records.FinishReviewHandoff(ctx, change); err != nil {
			return fmt.Errorf("save Pegasus review handoff: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("complete Pegasus review handoff: %w", err)
	}
	return nil
}

type reviewHandoffRecords struct{ executor dbexec.Executor }

func (records reviewHandoffRecords) CurrentReviewHandoff(
	ctx context.Context,
	id string,
) (application.ReviewHandoffSnapshot, error) {
	var result application.ReviewHandoffSnapshot
	var metadata, warnings string
	err := records.executor.QueryRowContext(ctx, `
 SELECT item.id,item.import_id,import.import_job_id,COALESCE(item.library_import_job_id,''),
 COALESCE(item.library_import_item_id,''),job.execution_no,job.attempt_count,COALESCE(job.worker_id,''),
 item.execution_state,import.state,job.state,item.version,import.version,
 COALESCE(job.leased_until_ms,0),COALESCE(job.execution_deadline_at_ms,0),item.metadata_json,item.warnings_json
 FROM pegasus_import_items item JOIN pegasus_imports import ON import.id=item.import_id
 JOIN jobs job ON job.id=import.import_job_id AND job.scope_type='PEGASUS_IMPORT'
 AND job.scope_id=import.id AND job.kind='SERVER_PEGASUS_IMPORT'
 WHERE item.id=?`, id).Scan(&result.Identity.ItemID, &result.Identity.ImportID, &result.Identity.JobID,
		&result.Identity.LibraryJobID, &result.Identity.LibraryItemID, &result.Identity.ExecutionNo, &result.Identity.Attempt,
		&result.Identity.WorkerID, &result.State, &result.ImportState, &result.JobState,
		&result.Version, &result.ImportVersion,
		&result.LeaseUntilMS, &result.DeadlineMS, &metadata, &warnings)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ReviewHandoffSnapshot{}, application.ErrNotFound
	}
	if err != nil {
		return application.ReviewHandoffSnapshot{}, fmt.Errorf("read Pegasus review source: %w", err)
	}
	if err := json.Unmarshal([]byte(metadata), &result.Metadata); err != nil {
		return application.ReviewHandoffSnapshot{}, fmt.Errorf("decode frozen Pegasus metadata: %w", err)
	}
	if err := decodeArray(warnings, &result.Warnings); err != nil {
		return application.ReviewHandoffSnapshot{}, fmt.Errorf("decode Pegasus review warnings: %w", err)
	}
	return result, nil
}

func (records reviewHandoffRecords) FinishReviewHandoff(
	ctx context.Context,
	change application.ReviewHandoffChange,
) error {
	before := change.Before
	identity := before.Identity
	encoded, err := json.Marshal(change.Warnings)
	if err != nil {
		return fmt.Errorf("encode Pegasus review warnings: %w", err)
	}
	result, err := recordstore.UpdatePegasusImportItems(ctx, records.executor, recordstore.Update{
		Set: `execution_state='REVIEW_PENDING',error_code=NULL,error_details_json=NULL,retryable=0,
 warnings_json=?,completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=? AND import_id=? AND version=? AND execution_state='VALIDATING'
 AND library_import_job_id=? AND library_import_item_id=?
 AND EXISTS(SELECT 1 FROM import_items WHERE id=? AND import_job_id=? AND state='REVIEW_PENDING')
 AND EXISTS(SELECT 1 FROM pegasus_imports import JOIN jobs job ON job.id=import.import_job_id
 WHERE import.id=? AND import.version=? AND import.state=? AND job.id=?
 AND job.state=? AND job.execution_no=? AND job.attempt_count=? AND job.worker_id=? AND job.leased_until_ms>?
 AND job.execution_deadline_at_ms>?)`, Args: []any{
			identity.ItemID, identity.ImportID, before.Version,
			identity.LibraryJobID, identity.LibraryItemID, identity.LibraryItemID, identity.LibraryJobID,
			identity.ImportID, before.ImportVersion, before.ImportState, identity.JobID, before.JobState,
			identity.ExecutionNo, identity.Attempt, identity.WorkerID, change.NowMS, change.NowMS,
		}},
		Values: []any{string(encoded), change.NowMS, change.NowMS},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	return RefreshCountsAndEvent(
		ctx,
		records.executor,
		identity.JobID,
		identity.ImportID,
		identity.ItemID,
		"REVIEW_PENDING",
		change.NowMS,
	)
}
