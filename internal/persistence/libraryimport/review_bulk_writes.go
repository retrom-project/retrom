package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	libraryservice "retrom/internal/service/libraryimport"
)

type ReviewBulkWrites struct{ executor dbapi.Executor }

func BindReviewBulkWrites(executor dbapi.Executor) *ReviewBulkWrites {
	return &ReviewBulkWrites{executor: executor}
}

func (repository *ReviewBulkWrites) CreateGlobal(
	ctx context.Context, bulkID, jobID, userID string, now int64,
) (libraryservice.ReviewBulkSummary, error) {
	var count int
	var maxID *string
	err := dbapi.QueryRowContext(ctx, repository.executor, `
SELECT count(*),max(id) FROM (
 SELECT id FROM import_items WHERE state='REVIEW_PENDING' AND review_version>0
 ORDER BY id LIMIT 10001
)`).Scan(&count, &maxID)
	if err != nil {
		return libraryservice.ReviewBulkSummary{}, fmt.Errorf("bound review queue: %w", err)
	}
	if count == 0 || maxID == nil {
		return libraryservice.ReviewBulkSummary{}, libraryservice.ErrReviewBulkEmpty
	}
	if count > 10000 {
		return libraryservice.ReviewBulkSummary{}, libraryservice.ErrReviewBulkTooLarge
	}
	dedupe := sha256.Sum256([]byte(bulkID))
	if _, err = repository.executor.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,
state,attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'REVIEW_BULK_APPROVE',?,1,'{"schemaVersion":1}',0,
'QUEUED',0,4,1,?,?,?)`, jobID, bulkID, hex.EncodeToString(dedupe[:]), now, now, now); err != nil {
		return libraryservice.ReviewBulkSummary{}, fmt.Errorf("create review bulk job: %w", err)
	}
	if _, err = recordstore.CreateReviewBulkApprovals(ctx, repository.executor, `
INSERT INTO review_bulk_approvals(id,job_id,state,max_item_id,initial_pending_count,
created_by_user_id,created_at_ms,updated_at_ms)
VALUES(?,?,'QUEUED',?,?,?,?,?)`, bulkID, jobID, *maxID, count, userID, now, now); err != nil {
		return libraryservice.ReviewBulkSummary{}, fmt.Errorf("create global review bulk: %w", err)
	}
	return libraryservice.ReviewBulkSummary{
		BulkApprovalID: bulkID, JobID: jobID, State: "QUEUED", Version: 1, MaxItemID: *maxID,
		InitialPendingCount: count, CreatedAtMS: now, UpdatedAtMS: now,
	}, nil
}
