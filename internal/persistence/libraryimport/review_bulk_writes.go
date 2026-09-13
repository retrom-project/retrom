package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/persistence/dbexec"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/libraryimport"
)

type ReviewBulkWrites struct{ executor dbexec.Executor }

func BindReviewBulkWrites(executor dbexec.Executor) *ReviewBulkWrites {
	return &ReviewBulkWrites{executor: executor}
}

func (repository *ReviewBulkWrites) Create(
	ctx context.Context,
	creation application.ReviewBulkCreation,
) (application.ReviewBulkSummary, error) {
	if _, err := repository.executor.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'REVIEW_BULK_APPROVE',?,1,?,1,'QUEUED',0,4,1,?,?,?)
`, creation.JobID, creation.BulkApprovalID, creation.DedupeKey, creation.PayloadJSON,
		creation.NowMS, creation.NowMS, creation.NowMS); err != nil {
		return application.ReviewBulkSummary{}, fmt.Errorf("create review bulk job: %w", err)
	}
	if _, err := repository.executor.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)
`, creation.JobID, creation.PayloadJSON, creation.InputDigest, creation.NowMS); err != nil {
		return application.ReviewBulkSummary{}, fmt.Errorf("create review bulk input: %w", err)
	}
	if _, err := repository.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'QUEUED',json_object('candidateCount',?),?)
`, creation.JobID, creation.BulkApprovalID, len(creation.Candidates), creation.NowMS); err != nil {
		return application.ReviewBulkSummary{}, fmt.Errorf("create review bulk event: %w", err)
	}
	if _, err := recordstore.CreateReviewBulkApprovals(ctx, repository.executor, `
INSERT INTO review_bulk_approvals(id,job_id,state,scope_json,scope_digest,candidate_manifest_digest,
matched_count,candidate_count,screenshot_only_count,duplicate_count,attachment_active_count,
source_flagged_count,not_ready_or_stale_count,created_by_user_id,version,created_at_ms,updated_at_ms)
VALUES(?,?,'QUEUED',?,?,?,?,?,?,?,?,?,?,?,1,?,?)
`, creation.BulkApprovalID, creation.JobID, creation.ScopeJSON, creation.ScopeDigest,
		creation.CandidateManifestDigest, creation.Counts.Matched, len(creation.Candidates),
		creation.Counts.ScreenshotOnly, creation.Counts.Duplicate, creation.Counts.AttachmentActive,
		creation.Counts.SourceFlagged, creation.Counts.NotReadyOrStale, creation.CreatedByUserID,
		creation.NowMS, creation.NowMS); err != nil {
		return application.ReviewBulkSummary{}, fmt.Errorf("create review bulk approval: %w", err)
	}
	for ordinal, candidate := range creation.Candidates {
		if _, err := recordstore.CreateReviewBulkApprovalItems(ctx, repository.executor, `
INSERT INTO review_bulk_approval_items(bulk_approval_id,import_item_id,ordinal,expected_review_version,
expected_validation_id,expected_source_snapshot_id,title_snapshot,target_platform_instance_id,
target_platform_name_snapshot,state,created_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,'PENDING',?)
`, creation.BulkApprovalID, candidate.ItemID, ordinal, candidate.ReviewVersion,
			optionalReviewBulkString(candidate.ValidationID), candidate.SourceSnapshotID,
			candidate.Title, candidate.PlatformInstanceID, candidate.PlatformName, creation.NowMS); err != nil {
			return application.ReviewBulkSummary{}, fmt.Errorf("create review bulk item: %w", err)
		}
	}
	return application.ReviewBulkSummary{
		BulkApprovalID: creation.BulkApprovalID, JobID: creation.JobID, State: "QUEUED", Version: 1,
		Scope: creation.Scope, Counts: creation.Counts,
		Progress:    application.ReviewBulkProgress{Candidate: len(creation.Candidates)},
		CreatedAtMS: creation.NowMS, UpdatedAtMS: creation.NowMS,
	}, nil
}

func optionalReviewBulkString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
