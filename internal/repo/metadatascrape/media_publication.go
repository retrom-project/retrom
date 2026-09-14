package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/model/metadatascrape"
	"retrom/internal/repo/blobcatalog"
	"retrom/internal/repo/recordstore"
)

func (records mediaRecords) Publish(ctx context.Context, value metadatascrape.AssetPublication, version int64) error {
	blobID, err := blobcatalog.EnsureRecord(ctx, records.executor, value.Blob, value.MediaType, value.Now)
	if err != nil {
		return fmt.Errorf("register media blob: %w", err)
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
		Set:    `status=?,error_code=?,fetched_at_ms=?,media_reserved_bytes=0,version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `id=? AND version=? AND status<>'READY'`, Args: []any{asset.ID, asset.Version}},
		Values: []any{state, code, now, now},
	}))
}
