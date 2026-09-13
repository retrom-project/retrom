package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/repo/recordstore"
	"retrom/internal/service/metadatascrape"
)

func (records initialRecords) Apply(ctx context.Context, change metadatascrape.InitialDraftChange) error {
	_, err := recordstore.UpdateReviewDrafts(ctx, records.transaction, recordstore.Update{
		Set: `selected_candidate_id=?,cover_candidate_asset_id=?,background_candidate_asset_id=?,
 metadata_json=?,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `id=?`, Args: []any{change.DraftID}},
		Values: []any{change.CandidateID, change.CoverID, change.BackgroundID, change.MetadataJSON, change.Now},
	})
	if err != nil {
		return fmt.Errorf("update initial review metadata: %w", err)
	}
	for _, asset := range change.Screenshots {
		_, err := records.transaction.ExecContext(
			ctx,
			`INSERT INTO review_draft_screenshot_assets
 (review_draft_id,ordinal,candidate_asset_id,created_at_ms) VALUES(?,?,?,?)`,
			change.DraftID,
			asset.Ordinal,
			asset.ID,
			change.Now,
		)
		if err != nil {
			return fmt.Errorf("insert initial review screenshot: %w", err)
		}
	}
	_, err = recordstore.UpdateImportItems(ctx, records.transaction, recordstore.Update{
		Set: `search_text=trim(search_text || ' ' || lower(?))`, Scope: recordstore.Scope{
			Where: `id=?`,
			Args: []any{
				change.ItemID,
			},
		}, Values: []any{
			change.Title,
		},
	})
	if err != nil {
		return fmt.Errorf("index initial review title: %w", err)
	}
	return nil
}

func (records initialRecords) Advance(ctx context.Context, change metadatascrape.InitialProgressChange) error {
	result, err := recordstore.UpdateImportItems(ctx, records.transaction, recordstore.Update{
		Set:    `state=?,failed_stage=?,last_error_code=?,version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `id=? AND state='SCRAPING'`, Args: []any{change.ItemID}},
		Values: []any{change.ItemState, change.FailedStage, change.ErrorCode, change.Now},
	})
	if err := scheduleChanged(result, err, metadatascrape.ErrInitialItemState); err != nil {
		return err
	}
	query := `UPDATE import_jobs SET running_item_count=running_item_count-1,
 review_pending_item_count=review_pending_item_count+?,failed_item_count=failed_item_count+?,
 cancelled_item_count=cancelled_item_count+?,state=?,version=version+1,updated_at_ms=?`
	args := []any{change.ReviewDelta, change.FailedDelta, change.CancelledDelta, change.JobState, change.Now}
	if change.ErrorCode != nil {
		query += `,last_error_code=?`
		args = append(args, *change.ErrorCode)
	}
	query += ` WHERE id=? AND version=? AND running_item_count=? AND running_item_count>0`
	args = append(args, change.ImportJobID, change.ExpectedVersion, change.ExpectedRunning)
	result, err = records.transaction.ExecContext(ctx, query, args...)
	return scheduleChanged(result, err, metadatascrape.ErrInitialProgressState)
}
