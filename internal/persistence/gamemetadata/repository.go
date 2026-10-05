package gamemetadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/persistence/filedeletion"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/gametitle"

	"retrom/internal/persistence/recordstore"
	payloadservice "retrom/internal/service/cleanupjobs"
	application "retrom/internal/service/gamemetadata"
)

type Repository struct {
	database dbapi.DB
	deletion payloadservice.DeletionStager
}

func New(database dbapi.DB, deletion payloadservice.DeletionStager) *Repository {
	return &Repository{database: database, deletion: deletion}
}

func (repository *Repository) WithCandidateApply(
	ctx context.Context, work func(application.CandidateApplyScope) error,
) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(transaction dbapi.Tx) error {
		scope := candidateApplyScope{transaction: transaction, deletion: repository.deletion}
		if err := work(scope); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit scrape candidate apply: %w", err)
	}
	return nil
}

type candidateApplyScope struct {
	transaction dbapi.Tx
	deletion    payloadservice.DeletionStager
}

func (scope candidateApplyScope) Load(
	ctx context.Context, gameID, candidateID string,
) (application.CandidateApplySnapshot, error) {
	var snapshot application.CandidateApplySnapshot
	var players, releaseYear sql.NullInt64
	err := dbapi.QueryRowContext(ctx, scope.transaction, `
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
		return application.CandidateApplySnapshot{}, fmt.Errorf(
			"read scrape candidate apply snapshot: %w",
			err,
		)
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
SELECT file_record FROM game_assets WHERE game_id=? AND kind=? ORDER BY ordinal,id
`, gameID, kind)
	if err != nil {
		return nil, fmt.Errorf("list replaced game assets: %w", err)
	}
	defer func() { cleanup.Error("close replaced game assets", rows.Close()) }()
	fileRecords := make([]string, 0)
	for rows.Next() {
		var fileRecord string
		if err := rows.Scan(&fileRecord); err != nil {
			return nil, fmt.Errorf("scan replaced game asset: %w", err)
		}
		fileRecords = append(fileRecords, fileRecord)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate replaced game assets: %w", err)
	}
	if _, err := recordstore.DeleteRows(
		ctx,
		scope.transaction,
		"game_assets",
		recordstore.Scope{Where: "game_id=? AND kind=?", Args: []any{gameID, kind}},
	); err != nil {
		return nil, fmt.Errorf("delete replaced game assets: %w", err)
	}
	return fileRecords, nil
}

func (scope candidateApplyScope) CreateSelectedGameAssets(
	ctx context.Context,
	gameID, candidateID string,
	selected []application.CandidateAssetSelection,
	now int64,
) ([]string, error) {
	createdIDs := make([]string, 0, len(selected))
	for _, choice := range selected {
		var fileRecord, kind, mediaType string
		var width, height int64
		err := dbapi.QueryRowContext(ctx, scope.transaction, `
SELECT file_record,kind_hint,width_px,height_px,media_type
FROM scrape_candidate_assets
WHERE id=? AND scrape_candidate_id=? AND status='READY'
`, choice.ID, candidateID).Scan(&fileRecord, &kind, &width, &height, &mediaType)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, application.ErrCandidateAsset
		}
		if err != nil {
			return nil, fmt.Errorf("read scrape candidate asset: %w", err)
		}
		if kind != choice.Kind || fileRecord != choice.SourceFile {
			return nil, application.ErrCandidateAsset
		}
		var runID string
		if err := dbapi.QueryRowContext(
			ctx,
			scope.transaction,
			`SELECT run.id FROM scrape_candidates candidate JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
WHERE candidate.id=? AND run.game_id=?`,
			candidateID,
			gameID,
		).Scan(&runID); err != nil {
			return nil, fmt.Errorf("read selected media owner: %w", err)
		}

		if _, err := recordstore.CreateGameAssets(ctx, scope.transaction, `
INSERT INTO game_assets(id,
game_id,
file_record,
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
		`, choice.AssetID, gameID, choice.File, choice.Kind, choice.Ordinal,
			width, height, mediaType, now); err != nil {
			return nil, fmt.Errorf("create game asset: %w", err)
		}
		createdIDs = append(createdIDs, choice.AssetID)
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

func (scope candidateApplyScope) StageCandidates(
	ctx context.Context,
	gameID string,
	ids []string,
	_ int64,
) error {
	removed := make([]string, 0, len(ids))
	for _, id := range ids {
		var count int
		if err := dbapi.QueryRowContext(ctx, scope.transaction, `SELECT count(*) FROM game_assets
 WHERE game_id=? AND ((file_record)::jsonb #>> '{path}')=((?)::jsonb #>> '{path}')
`, gameID, id).Scan(&count); err != nil {
			return fmt.Errorf("stage candidates: %w", err)
		}
		if count == 0 {
			removed = append(removed, id)
		}
	}
	ids = removed
	if len(ids) == 0 || scope.deletion == nil {
		return nil
	}
	if err := scope.deletion.StageInScope(ctx, filedeletion.Bind(scope.transaction), ids); err != nil {
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

func (repository *Repository) SelectedFiles(ctx context.Context, candidateID string,
	selected []application.CandidateAssetSelection,
) ([]application.CandidateAssetSelection, error) {
	for i := range selected {
		choice := &selected[i]
		if err := dbapi.QueryRowContext(ctx, repository.database, `SELECT file_record FROM scrape_candidate_assets
 WHERE id=? AND scrape_candidate_id=? AND status='READY' AND kind_hint=?
`, choice.ID, candidateID, choice.Kind).Scan(&choice.SourceFile); err != nil {
			return nil, fmt.Errorf("selected files: %w", err)
		}
	}
	return selected, nil
}
