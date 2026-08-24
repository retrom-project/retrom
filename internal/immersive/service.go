package immersive

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type Service struct {
	database *sql.DB
}

type querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func New(database *sql.DB) *Service {
	return &Service{database: database}
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func queryPlatforms(ctx context.Context, database querier, profileID string) ([]Platform, error) {
	rows, err := database.QueryContext(ctx, `
SELECT platform.id,
       platform.name,
       count(*),
       max(profile_play.last_played_at_ms)
FROM platforms platform
JOIN platform_instances instance ON instance.platform_id=platform.id
JOIN games game ON game.platform_instance_id=instance.id
LEFT JOIN (
  SELECT session.game_id,max(session.started_at_ms) AS last_played_at_ms
  FROM play_sessions session
  WHERE session.profile_id=?
  GROUP BY session.game_id
) profile_play ON profile_play.game_id=game.id
WHERE game.status='PUBLISHED'
AND instance.enabled=1
GROUP BY platform.id,platform.name
HAVING count(*)>0
ORDER BY platform.name COLLATE NOCASE,platform.id
`, profileID)
	if err != nil {
		return nil, fmt.Errorf("immersive: query platforms: %w", err)
	}
	defer func() { _ = rows.Close() }()
	platforms := make([]Platform, 0)
	for rows.Next() {
		var platform Platform
		var lastPlayedAtMS sql.NullInt64
		if err := rows.Scan(
			&platform.ID,
			&platform.Name,
			&platform.GameCount,
			&lastPlayedAtMS,
		); err != nil {
			return nil, fmt.Errorf("immersive: scan platform: %w", err)
		}
		platform.LastPlayedAtMS = nullableInt64Pointer(lastPlayedAtMS)
		platforms = append(platforms, platform)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("immersive: iterate platforms: %w", err)
	}
	return platforms, nil
}

func (service *Service) Platforms(ctx context.Context, profileID string) ([]Platform, error) {
	return queryPlatforms(ctx, service.database, profileID)
}

func queryPlatform(
	ctx context.Context,
	database querier,
	profileID, platformID string,
) (Platform, error) {
	var platform Platform
	var lastPlayedAtMS sql.NullInt64
	err := database.QueryRowContext(ctx, `
SELECT platform.id,
       platform.name,
       count(*),
       max(profile_play.last_played_at_ms)
FROM platforms platform
JOIN platform_instances instance ON instance.platform_id=platform.id
JOIN games game ON game.platform_instance_id=instance.id
LEFT JOIN (
  SELECT session.game_id,max(session.started_at_ms) AS last_played_at_ms
  FROM play_sessions session
  WHERE session.profile_id=?
  GROUP BY session.game_id
) profile_play ON profile_play.game_id=game.id
WHERE platform.id=?
AND game.status='PUBLISHED'
AND instance.enabled=1
GROUP BY platform.id,platform.name
HAVING count(*)>0
`, profileID, platformID).Scan(
		&platform.ID,
		&platform.Name,
		&platform.GameCount,
		&lastPlayedAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Platform{}, ErrPlatformNotFound
	}
	if err != nil {
		return Platform{}, fmt.Errorf("immersive: query platform: %w", err)
	}
	platform.LastPlayedAtMS = nullableInt64Pointer(lastPlayedAtMS)
	return platform, nil
}

func queryGames(
	ctx context.Context,
	database querier,
	profileID, platformID string,
	limit int,
	cursor *GameCursor,
) ([]Game, error) {
	query := `
SELECT game.id,
       metadata.title,
       metadata.description,
       metadata.release_year,
       metadata.developer,
       metadata.genre,
       instance.id,
       instance.name,
       core.id,
       core.name,
       (SELECT asset.id
        FROM game_assets asset
        WHERE asset.game_id=game.id
        AND asset.metadata_revision_id=game.current_metadata_revision_id
        AND asset.kind='COVER'
        AND asset.ordinal=0
        LIMIT 1),
       (SELECT asset.id
        FROM game_assets asset
        WHERE asset.game_id=game.id
        AND asset.metadata_revision_id=game.current_metadata_revision_id
        AND asset.kind='VIDEO'
        AND asset.ordinal=0
        LIMIT 1),
       (SELECT max(session.started_at_ms)
        FROM play_sessions session
        WHERE session.game_id=game.id
        AND session.profile_id=?)
FROM games game
JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN cores core ON core.id=instance.default_core_id
WHERE instance.platform_id=?
AND instance.enabled=1
AND game.status='PUBLISHED'
`
	arguments := []any{profileID, platformID}
	if cursor != nil {
		query += `AND (
 metadata.title COLLATE NOCASE > ? COLLATE NOCASE
 OR (metadata.title COLLATE NOCASE = ? COLLATE NOCASE AND game.id>?)
)
`
		arguments = append(arguments, cursor.Title, cursor.Title, cursor.ID)
	}
	query += "ORDER BY metadata.title COLLATE NOCASE,game.id LIMIT ?"
	arguments = append(arguments, limit+1)
	rows, err := database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("immersive: query games: %w", err)
	}
	defer func() { _ = rows.Close() }()
	games := make([]Game, 0, limit+1)
	for rows.Next() {
		var game Game
		var releaseYear, lastPlayedAtMS sql.NullInt64
		var coverAssetID, videoAssetID sql.NullString
		if err := rows.Scan(
			&game.ID,
			&game.Title,
			&game.Description,
			&releaseYear,
			&game.Developer,
			&game.Genre,
			&game.PlatformInstance.ID,
			&game.PlatformInstance.Name,
			&game.DefaultCore.ID,
			&game.DefaultCore.Name,
			&coverAssetID,
			&videoAssetID,
			&lastPlayedAtMS,
		); err != nil {
			return nil, fmt.Errorf("immersive: scan game: %w", err)
		}
		game.ReleaseYear = nullableInt64Pointer(releaseYear)
		game.CoverAssetID = nullableStringPointer(coverAssetID)
		game.VideoAssetID = nullableStringPointer(videoAssetID)
		game.LastPlayedAtMS = nullableInt64Pointer(lastPlayedAtMS)
		games = append(games, game)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("immersive: iterate games: %w", err)
	}
	return games, nil
}

func (service *Service) Games(
	ctx context.Context,
	profileID, platformID string,
	limit int,
	cursor *GameCursor,
) (GamePage, error) {
	transaction, err := service.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return GamePage{}, fmt.Errorf("immersive: begin read transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	platform, err := queryPlatform(ctx, transaction, profileID, platformID)
	if err != nil {
		return GamePage{}, err
	}
	games, err := queryGames(ctx, transaction, profileID, platformID, limit, cursor)
	if err != nil {
		return GamePage{}, err
	}
	var next *GameCursor
	if len(games) > limit {
		last := games[limit-1]
		next = &GameCursor{Title: last.Title, ID: last.ID}
		games = games[:limit]
	}
	if err := transaction.Commit(); err != nil {
		return GamePage{}, fmt.Errorf("immersive: commit read transaction: %w", err)
	}
	return GamePage{Platform: platform, Items: games, NextCursor: next}, nil
}
