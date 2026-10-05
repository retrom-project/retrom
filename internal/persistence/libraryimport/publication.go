package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/service/importprogress"
	libraryservice "retrom/internal/service/libraryimport"
)

func (records reviewApprovalRecords) ReadPublication(ctx context.Context,
	id string,
) (libraryservice.PublicationState, error) {
	var result libraryservice.PublicationState
	var encoded sql.NullString
	err := dbapi.QueryRowContext(ctx, records.transaction, `
SELECT CASE WHEN state='DISCARDED' AND EXISTS(SELECT 1 FROM import_item_duplicate_matches
 WHERE import_item_id=import_items.id)
 THEN 'SKIPPED_EXISTING' ELSE state END,
 COALESCE(publication_game_id,(SELECT existing_game_id FROM import_item_duplicate_matches
 WHERE import_item_id=import_items.id ORDER BY existing_game_id LIMIT 1),''),publication_json
 FROM import_items WHERE id=?
`, id).Scan(&result.State, &result.GameID, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read item publication: %w", err)
	}
	result.Found = true
	if encoded.Valid {
		var intent libraryservice.Publication
		if err := json.Unmarshal([]byte(encoded.String), &intent); err != nil {
			return result, fmt.Errorf("decode publication: %w", err)
		}
		result.Intent = &intent
	}
	return result, nil
}

func (records reviewApprovalRecords) BeginPublication(ctx context.Context,
	intent libraryservice.Publication, now int64,
) error {
	encoded, err := json.Marshal(intent)
	if err != nil {
		return fmt.Errorf("encode publication: %w", err)
	}
	{
		var pending bool
		if err := dbapi.QueryRowContext(ctx, records.transaction, `SELECT EXISTS(SELECT 1 FROM import_items
   WHERE state='PUBLISHING' AND ((publication_json)::jsonb #>> '{Head,PlatformID}')=? AND
((publication_json)::jsonb #>> '{IdentityDigest}')=? AND id<>?)`,
			intent.Head.PlatformID, intent.IdentityDigest, intent.Request.ItemID).Scan(&pending); err != nil {
			return fmt.Errorf("read concurrent publication: %w", err)
		}
		if pending {
			return libraryservice.ErrPublicationBusy
		}
	}
	var bulkID any
	if intent.Request.Bulk != nil {
		bulkID = intent.Request.Bulk.BulkID
	}
	result, err := records.transaction.ExecContext(ctx, `UPDATE import_items
 SET state='PUBLISHING',publication_game_id=?,publication_bulk_id=?,publication_json=?,updated_at_ms=?
 WHERE id=? AND state='REVIEW_PENDING' AND review_version=? AND publication_json IS NULL`,
		intent.GameID, bulkID, string(encoded), now, intent.Request.ItemID, intent.Request.ExpectedVersion)
	return approvalMutation(result, err, "freeze reviewed publication", true)
}

func (records reviewApprovalRecords) PublicationProgress(ctx context.Context,
	id string,
) (importprogress.Snapshot, int64, error) {
	var snapshot importprogress.Snapshot
	var version int64
	err := dbapi.QueryRowContext(ctx, records.transaction, `SELECT version,state,queued_item_count,running_item_count,
 review_pending_item_count,failed_item_count,cancelled_item_count,rejected_file_count,
resolved_rejected_file_count,
 cancel_requested_at_ms,completed_at_ms FROM import_jobs WHERE id=?`, id).Scan(&version, &snapshot.State,
		&snapshot.Counts.Queued, &snapshot.Counts.Running, &snapshot.Counts.ReviewPending, &snapshot.Counts.Failed,
		&snapshot.Counts.Cancelled, &snapshot.Counts.Rejected, &snapshot.Counts.ResolvedRejected,
		&snapshot.CancelRequestedAtMS, &snapshot.CompletedAtMS)
	if err != nil {
		return snapshot, 0, fmt.Errorf("read publication progress: %w", err)
	}
	return snapshot, version, nil
}

func (repository *ReviewApprovals) PendingPublications(
	ctx context.Context,
) ([]libraryservice.ReviewApprovalRequest, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT publication_json FROM import_items WHERE state='PUBLISHING' ORDER BY id
`)
	if err != nil {
		return nil, fmt.Errorf("read pending publications: %w", err)
	}
	defer func() { cleanup.Error("close pending publications", rows.Close()) }()
	var requests []libraryservice.ReviewApprovalRequest
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			return nil, fmt.Errorf("read publication intent: %w", err)
		}
		var intent libraryservice.Publication
		if err := json.Unmarshal([]byte(encoded), &intent); err != nil {
			return nil, fmt.Errorf("decode pending publication: %w", err)
		}
		requests = append(requests, intent.Request)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending publications: %w", err)
	}
	return requests, nil
}
