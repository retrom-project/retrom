package metadatascrape

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/persistence/blobcatalog"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/metadatascrape"
)

type AssetRepository struct{ database *sql.DB }

func NewAssets(database *sql.DB) *AssetRepository { return &AssetRepository{database: database} }

func (repository *AssetRepository) Publish(ctx context.Context, value metadatascrape.AssetPublication) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin metadata asset publication: %w", err)
	}
	defer dbexec.Rollback(transaction)
	blobID, err := blobcatalog.EnsureRecord(ctx, transaction, value.Blob, value.MediaType, value.Now)
	if err != nil {
		return fmt.Errorf("register metadata asset blob: %w", err)
	}
	result, err := recordstore.UpdateScrapeCandidateAssets(ctx, transaction, recordstore.Update{
		Set: `status='READY',blob_id=?,width_px=?,height_px=?,media_type=?,fetched_at_ms=?,
 version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=? AND status='PENDING' AND EXISTS(
 SELECT 1 FROM scrape_candidates candidate JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
 LEFT JOIN games game ON game.id=run.game_id WHERE candidate.id=scrape_candidate_assets.scrape_candidate_id
 AND (run.game_id IS NULL OR game.status='PUBLISHED'))`, Args: []any{value.ID}},
		Values: []any{blobID, value.Width, value.Height, value.MediaType, value.Now, value.Now},
	})
	if err != nil {
		return fmt.Errorf("attach metadata asset blob: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count metadata asset publication: %w", err)
	}
	if changed != 1 {
		return metadatascrape.ErrGameDeleted
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit metadata asset publication: %w", err)
	}
	return nil
}

func (repository *AssetRepository) Fail(ctx context.Context, id, code string, now int64) error {
	result, err := recordstore.UpdateScrapeCandidateAssets(ctx, repository.database, recordstore.Update{
		Set:   `status='FAILED',error_code=?,fetched_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=? AND status='PENDING'`, Args: []any{id}}, Values: []any{code, now, now},
	})
	if err != nil {
		return fmt.Errorf("fail metadata asset: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count failed metadata assets: %w", err)
	}
	if changed != 1 {
		return metadatascrape.ErrAssetStateConflict
	}
	return nil
}
