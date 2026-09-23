package maintenance

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/maintenance"
)

func (records reviewRecords) Complete(ctx context.Context, change application.RestoredReviewChange) error {
	before := change.Before
	if err := records.fence(ctx, before); err != nil {
		return err
	}
	for _, state := range change.Preparation {
		if err := records.prepare(ctx, before, state, change.NowMS); err != nil {
			return err
		}
		before.State, before.Version, before.Retryable = state, before.Version+1, false
	}
	if err := records.fence(ctx, before); err != nil {
		return err
	}
	result, err := records.update(ctx, before.Kind, recordstore.Update{
		Set: `execution_state='REVIEW_PENDING',library_import_job_id=?,library_import_item_id=?,
warnings_json=?,error_code=NULL,error_details_json=NULL,retryable=0,completed_at_ms=?,
version=version+1,updated_at_ms=?`,
		Values: []any{before.ReservedJobID, before.ReservedItemID, change.WarningsJSON, change.NowMS, change.NowMS},
		Scope: recordstore.Scope{
			Where: `id=? AND import_id=? AND version=? AND execution_state='VALIDATING'
AND metadata_json=? AND warnings_json=? AND library_import_job_id IS ? AND library_import_item_id IS ?
AND EXISTS(SELECT 1 FROM import_items WHERE id=? AND import_job_id=?
AND state='REVIEW_PENDING' AND version=? AND review_version=?)`,
			Args: []any{
				before.ItemID, before.ImportID, before.Version, before.MetadataJSON, before.WarningsJSON,
				nullableReviewID(before.LibraryJobID), nullableReviewID(before.LibraryItemID),
				before.ReservedItemID, before.ReservedJobID, before.OrdinaryVersion, before.OrdinaryReviewVersion,
			},
		},
	})
	if err := requireRestoredReview(result, err); err != nil {
		return err
	}
	return records.progress(ctx, before, change.NowMS)
}

func (records reviewRecords) Verify(ctx context.Context, before application.RestoredReview) error {
	return records.fence(ctx, before)
}

func (records reviewRecords) fence(ctx context.Context, before application.RestoredReview) error {
	current, err := records.current(ctx, before.Kind, before.ItemID)
	if err != nil {
		return err
	}
	if current != before {
		return application.ErrInvalidBundle
	}
	return nil
}

func (records reviewRecords) prepare(
	ctx context.Context, before application.RestoredReview, state string, now int64,
) error {
	result, err := records.update(ctx, before.Kind, recordstore.Update{
		Set: `execution_state=?,version=version+1,updated_at_ms=?,error_code=NULL,error_details_json=NULL,
retryable=0,completed_at_ms=NULL`,
		Values: []any{state, now},
		Scope: recordstore.Scope{
			Where: `id=? AND import_id=? AND version=? AND execution_state=? AND retryable=?`,
			Args:  []any{before.ItemID, before.ImportID, before.Version, before.State, before.Retryable},
		},
	})
	return requireRestoredReview(result, err)
}

func (records reviewRecords) update(
	ctx context.Context, kind string, change recordstore.Update,
) (sql.Result, error) {
	var result sql.Result
	var err error
	switch kind {
	case "SOURCE":
		result, err = recordstore.UpdateSourceImportItems(ctx, records.executor, change)

	default:
		return nil, application.ErrInvalidBundle
	}
	if err != nil {
		return nil, fmt.Errorf("update restored source review: %w", err)
	}
	return result, nil
}

func (records reviewRecords) progress(ctx context.Context, before application.RestoredReview, now int64) error {
	encoded, err := json.Marshal(struct {
		SchemaVersion int    `json:"schemaVersion"`
		ItemID        string `json:"itemId"`
		Outcome       string `json:"outcome"`
	}{1, before.ItemID, "REVIEW_PENDING"})
	if err != nil {
		return fmt.Errorf("encode restored review progress: %w", err)
	}
	result, err := records.executor.ExecContext(ctx, `INSERT INTO job_events
(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES(?,?,?,'PROGRESS',?,?)`,
		before.JobID, before.Kind+"_IMPORT", before.ImportID, string(encoded), now)
	return requireRestoredReview(result, err)
}

func requireRestoredReview(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("write restored review handoff: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count restored review handoff: %w", err)
	}
	if count != 1 {
		return application.ErrInvalidBundle
	}
	return nil
}

func nullableReviewID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}
