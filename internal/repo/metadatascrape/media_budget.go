package metadatascrape

import (
	"context"

	"retrom/internal/service/metadatascrape"
)

func (records mediaRecords) Reserve(ctx context.Context, asset metadatascrape.MediaAsset, amount, now int64) error {
	if err := mediaChanged(records.executor.ExecContext(ctx, `
UPDATE metadata_media_runs SET charged_bytes=charged_bytes+?,
 version=version+1,updated_at_ms=? WHERE scrape_run_id=? AND charged_bytes+?<=?`, amount, now, asset.RunID,
		amount, metadatascrape.MediaRunBudget)); err != nil {
		return err
	}
	// A previous process may have died after its read. Its reservation remains charged; a new attempt pays separately.
	return mediaChanged(records.executor.ExecContext(ctx, `
UPDATE scrape_candidate_assets SET status='FETCHING',error_code=NULL,
 fetched_at_ms=NULL,media_charged_bytes=media_charged_bytes+?,media_reserved_bytes=?,version=version+1,updated_at_ms=?
 WHERE id=? AND version=? AND status IN ('PENDING','FETCHING','FAILED')`, amount, amount, now, asset.ID, asset.Version))
}

func (records mediaRecords) Account(ctx context.Context, asset metadatascrape.MediaAsset, received, now int64) error {
	refund := asset.Reserved - received
	if err := mediaChanged(records.executor.ExecContext(ctx, `
UPDATE metadata_media_runs SET charged_bytes=charged_bytes-?,
 version=version+1,updated_at_ms=? WHERE scrape_run_id=? AND charged_bytes>=?`,
		refund, now, asset.RunID, refund)); err != nil {
		return err
	}
	return mediaChanged(records.executor.ExecContext(ctx, `
UPDATE scrape_candidate_assets SET media_charged_bytes=media_charged_bytes-?,
 media_reserved_bytes=0,version=version+1,updated_at_ms=? WHERE id=? AND version=? AND media_reserved_bytes=?`,
		refund, now, asset.ID, asset.Version, asset.Reserved))
}
