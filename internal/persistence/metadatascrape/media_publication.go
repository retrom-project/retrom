package metadatascrape

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"

	"retrom/internal/persistence/filecatalog"
	"retrom/internal/persistence/fileownership"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/metadatascrape"
)

func (records mediaRecords) Publish(
	ctx context.Context,
	value metadatascrape.AssetPublication,
	version int64,
) error {
	blobID, err := filecatalog.EnsureRecord(ctx, records.executor, value.Blob, value.MediaType, value.Now)
	if err != nil {
		return fmt.Errorf("register media blob: %w", err)
	}
	var owner fileownership.Owner
	if err := dbapi.QueryRowContext(
		ctx,
		records.executor,
		`SELECT CASE WHEN run.import_item_id IS NULL THEN 'SCRAPE_RUN' ELSE 'IMPORT_ITEM' END,
 COALESCE(run.import_item_id,run.id) FROM scrape_candidate_assets asset JOIN scrape_candidates
candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id WHERE asset.id=?`,
		value.ID,
	).Scan(&owner.Kind, &owner.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return metadatascrape.ErrExecutionLost
		}
		return fmt.Errorf("read candidate media owner: %w", err)
	}
	if err := fileownership.Adopt(ctx, records.executor, blobID, owner); err != nil {
		return fmt.Errorf("media publication: %w", err)
	}
	return mediaChanged(recordstore.UpdateScrapeCandidateAssets(ctx, records.executor, recordstore.Update{
		Set: `status='READY',blob_id=?,width_px=?,height_px=?,media_type=?,
 fetched_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND status='FETCHING' AND media_reserved_bytes=0`,
			Args:  []any{value.ID, version},
		},
		Values: []any{blobID, value.Width, value.Height, value.MediaType, value.Now, value.Now},
	}))
}

func (records mediaRecords) Fail(
	ctx context.Context, asset metadatascrape.MediaAsset, state, code string, now int64,
) error {
	return mediaChanged(recordstore.UpdateScrapeCandidateAssets(ctx, records.executor, recordstore.Update{
		Set: `status=?,error_code=?,fetched_at_ms=?,media_reserved_bytes=0,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND status<>'READY'`,
			Args:  []any{asset.ID, asset.Version},
		},
		Values: []any{state, code, now, now},
	}))
}
