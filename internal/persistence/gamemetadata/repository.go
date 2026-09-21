package gamemetadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	"retrom/internal/gametitle"
	"retrom/internal/persistence/payloadrelease"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/gamemetadata"
	payloadservice "retrom/internal/service/payloadrelease"

	"github.com/google/uuid"
)

type Repository struct {
	database *sql.DB
	gc       payloadservice.GCStager
}

func New(database *sql.DB, gc payloadservice.GCStager) *Repository {
	return &Repository{database: database, gc: gc}
}

func (repository *Repository) WithCandidateApply(
	ctx context.Context, work func(application.CandidateApplyScope) error,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin scrape candidate apply: %w", err)
	}
	defer dbexec.Rollback(transaction)
	scope := candidateApplyScope{transaction: transaction, gc: repository.gc}
	if err := work(scope); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit scrape candidate apply: %w", err)
	}
	return nil
}

type candidateApplyScope struct {
	transaction *sql.Tx
	gc          payloadservice.GCStager
}

func (scope candidateApplyScope) Load(
	ctx context.Context, gameID, candidateID string,
) (application.CandidateApplySnapshot, error) {
	var snapshot application.CandidateApplySnapshot
	var players, releaseYear sql.NullInt64
	err := scope.transaction.QueryRowContext(ctx, `
SELECT g.version,
g.title,
g.description,
g.developer,
g.publisher,
g.genre,
g.players,
g.release_year,
c.normalized_metadata_json
FROM games g
JOIN scrape_candidates c ON c.id=?
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
AND r.game_id=g.id
AND r.state='COMPLETED'
WHERE g.id=?
AND g.status='PUBLISHED'
AND r.id=(SELECT id
FROM metadata_scrape_runs
WHERE game_id=g.id
AND provider='HASHEOUS'
AND state='COMPLETED'
ORDER BY created_at_ms DESC,
id DESC LIMIT 1)
`, candidateID, gameID).Scan(
		&snapshot.Version,
		&snapshot.Current.Title,
		&snapshot.Current.Description,
		&snapshot.Current.Developer,
		&snapshot.Current.Publisher,
		&snapshot.Current.Genre,
		&players,
		&releaseYear,
		&snapshot.CandidateMetadataJSON,
	)
	if err != nil {
		return application.CandidateApplySnapshot{}, fmt.Errorf("read scrape candidate apply snapshot: %w", err)
	}
	if players.Valid {
		value := players.Int64
		snapshot.Current.Players = &value
	}
	if releaseYear.Valid {
		value := releaseYear.Int64
		snapshot.Current.ReleaseYear = &value
	}
	return snapshot, nil
}

func (scope candidateApplyScope) ReplaceGameAssets(
	ctx context.Context, gameID, kind string,
) ([]string, error) {
	rows, err := scope.transaction.QueryContext(ctx, `
SELECT blob_id FROM game_assets WHERE game_id=? AND kind=? ORDER BY ordinal,id
`, gameID, kind)
	if err != nil {
		return nil, fmt.Errorf("list replaced game assets: %w", err)
	}
	defer func() { cleanup.Error("close replaced game assets", rows.Close()) }()
	blobIDs := make([]string, 0)
	for rows.Next() {
		var blobID string
		if err := rows.Scan(&blobID); err != nil {
			return nil, fmt.Errorf("scan replaced game asset: %w", err)
		}
		blobIDs = append(blobIDs, blobID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate replaced game assets: %w", err)
	}
	if _, err := scope.transaction.ExecContext(
		ctx, `DELETE FROM game_assets WHERE game_id=? AND kind=?`, gameID, kind,
	); err != nil {
		return nil, fmt.Errorf("delete replaced game assets: %w", err)
	}
	return blobIDs, nil
}

func (scope candidateApplyScope) CreateSelectedGameAssets(
	ctx context.Context, gameID, candidateID string, selected []application.CandidateAssetSelection, now int64,
) ([]string, error) {
	createdIDs := make([]string, 0, len(selected))
	for _, choice := range selected {
		var blobID, kind, mediaType string
		var width, height int64
		err := scope.transaction.QueryRowContext(ctx, `
SELECT blob_id,kind_hint,width_px,height_px,media_type
FROM scrape_candidate_assets
WHERE id=? AND scrape_candidate_id=? AND status='READY'
`, choice.ID, candidateID).Scan(&blobID, &kind, &width, &height, &mediaType)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, application.ErrCandidateAsset
		}
		if err != nil {
			return nil, fmt.Errorf("read scrape candidate asset: %w", err)
		}
		if kind != choice.Kind {
			return nil, application.ErrCandidateAsset
		}
		assetID, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("create game asset identity: %w", err)
		}
		if _, err := recordstore.CreateGameAssets(ctx, scope.transaction, `
INSERT INTO game_assets(id,
game_id,
blob_id,
kind,
ordinal,
width_px,
height_px,
media_type,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?)
		`, assetID.String(), gameID, blobID, choice.Kind, choice.Ordinal,
			width, height, mediaType, now); err != nil {
			return nil, fmt.Errorf("create game asset: %w", err)
		}
		createdIDs = append(createdIDs, assetID.String())
	}
	return createdIDs, nil
}

func (scope candidateApplyScope) UpdateGameMetadata(
	ctx context.Context, update application.GameMetadataUpdate,
) (bool, error) {
	result, err := recordstore.UpdateGames(ctx, scope.transaction, recordstore.Update{
		Set: `
title=?,
title_initial=?,
description=?,
developer=?,
publisher=?,
genre=?,
players=?,
release_year=?,
metadata_source_kind='RESCRAPE_APPLY',
metadata_source_ref_id=?,
search_text=?,
version=version+1,
updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
id=?
AND version=?
`,
			Args: []any{update.GameID, update.ExpectedVersion},
		},
		Values: []any{
			update.Metadata.Title,
			gametitle.Initial(update.Metadata.Title),
			update.Metadata.Description,
			update.Metadata.Developer,
			update.Metadata.Publisher,
			update.Metadata.Genre,
			nullable(update.Metadata.Players),
			nullable(update.Metadata.ReleaseYear),
			update.CandidateID,
			searchText(update.Metadata),
			update.NowMS,
		},
	})
	if err != nil {
		return false, fmt.Errorf("update game metadata: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count game metadata update: %w", err)
	}
	return changed == 1, nil
}

func (scope candidateApplyScope) StageCandidates(ctx context.Context, ids []string) error {
	if len(ids) == 0 || scope.gc == nil {
		return nil
	}
	if err := scope.gc.StageInScope(ctx, payloadrelease.BindGC(scope.transaction), ids); err != nil {
		return fmt.Errorf("stage candidate payloads: %w", err)
	}
	return nil
}

func nullable(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func searchText(metadata application.Metadata) string {
	return strings.ToLower(strings.Join([]string{
		metadata.Title, metadata.Developer, metadata.Publisher, metadata.Genre,
	}, " "))
}

var _ application.CandidateApplyRepository = (*Repository)(nil)
