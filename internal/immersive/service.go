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
	transaction, err := service.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("immersive: begin platform transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	platforms, err := queryPlatforms(ctx, transaction, profileID)
	if err != nil {
		return nil, err
	}
	featuredGames, err := queryFeaturedGames(ctx, transaction, profileID, "")
	if err != nil {
		return nil, err
	}
	attachFeaturedGames(platforms, featuredGames)
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("immersive: commit platform transaction: %w", err)
	}
	return platforms, nil
}

func queryFeaturedGames(
	ctx context.Context,
	database querier,
	profileID, platformID string,
) ([]FeaturedGame, error) {
	query := `
WITH profile_play AS (
  SELECT session.game_id,max(session.started_at_ms) AS last_played_at_ms
  FROM play_sessions session
  WHERE session.profile_id=?
  GROUP BY session.game_id
), ranked AS (
  SELECT instance.platform_id,
         game.id,
         metadata.title,
         (SELECT asset.id
          FROM game_assets asset
          WHERE asset.game_id=game.id
          AND asset.game_id=game.id
          AND asset.kind='COVER'
          AND asset.ordinal=0
          LIMIT 1) AS cover_asset_id,
         profile_play.last_played_at_ms,
         row_number() OVER (
           PARTITION BY instance.platform_id
           ORDER BY CASE WHEN profile_play.last_played_at_ms IS NULL THEN 1 ELSE 0 END,
                    profile_play.last_played_at_ms DESC,
                    game.created_at_ms DESC,
                    game.id DESC
         ) AS platform_rank
  FROM games game
  JOIN games metadata ON metadata.id=game.id
  JOIN platform_instances instance ON instance.id=game.platform_instance_id
  LEFT JOIN profile_play ON profile_play.game_id=game.id
  WHERE game.status='PUBLISHED'
  AND instance.enabled=1
`
	arguments := []any{profileID}
	if platformID != "" {
		query += "  AND instance.platform_id=?\n"
		arguments = append(arguments, platformID)
	}
	query += `)
SELECT platform_id,id,title,cover_asset_id,last_played_at_ms
FROM ranked
WHERE platform_rank<=3
ORDER BY platform_id,platform_rank
`
	rows, err := database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("immersive: query featured games: %w", err)
	}
	defer func() { _ = rows.Close() }()
	games := make([]FeaturedGame, 0)
	for rows.Next() {
		var game FeaturedGame
		var coverAssetID sql.NullString
		var lastPlayedAtMS sql.NullInt64
		if err := rows.Scan(
			&game.PlatformID,
			&game.ID,
			&game.Title,
			&coverAssetID,
			&lastPlayedAtMS,
		); err != nil {
			return nil, fmt.Errorf("immersive: scan featured game: %w", err)
		}
		game.CoverAssetID = nullableStringPointer(coverAssetID)
		game.LastPlayedAtMS = nullableInt64Pointer(lastPlayedAtMS)
		games = append(games, game)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("immersive: iterate featured games: %w", err)
	}
	return games, nil
}

func attachFeaturedGames(platforms []Platform, games []FeaturedGame) {
	platformIndexes := make(map[string]int, len(platforms))
	for index := range platforms {
		platforms[index].FeaturedGames = make([]FeaturedGame, 0, 3)
		platformIndexes[platforms[index].ID] = index
	}
	for _, game := range games {
		index, found := platformIndexes[game.PlatformID]
		if found {
			platforms[index].FeaturedGames = append(platforms[index].FeaturedGames, game)
		}
	}
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
       metadata.title_initial,
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
        AND asset.game_id=game.id
        AND asset.kind='COVER'
        AND asset.ordinal=0
        LIMIT 1),
       (SELECT asset.id
        FROM game_assets asset
        WHERE asset.game_id=game.id
        AND asset.game_id=game.id
        AND asset.kind='VIDEO'
        AND asset.ordinal=0
        LIMIT 1),
       (SELECT max(session.started_at_ms)
        FROM play_sessions session
        WHERE session.game_id=game.id
        AND session.profile_id=?),
       EXISTS(
         SELECT 1 FROM favorite_games favorite
         WHERE favorite.profile_id=? AND favorite.game_id=game.id
       )
FROM games game
JOIN games metadata ON metadata.id=game.id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN cores core ON core.id=instance.default_core_id
WHERE instance.platform_id=?
AND instance.enabled=1
AND game.status='PUBLISHED'
`
	arguments := []any{profileID, profileID, platformID}
	if cursor != nil {
		query += `AND (
 metadata.title_initial>?
 OR (metadata.title_initial=? AND metadata.title COLLATE NOCASE> ? COLLATE NOCASE)
 OR (metadata.title_initial=? AND metadata.title COLLATE NOCASE=? COLLATE NOCASE AND game.id>?)
)
`
		arguments = append(
			arguments,
			cursor.TitleInitial,
			cursor.TitleInitial,
			cursor.Title,
			cursor.TitleInitial,
			cursor.Title,
			cursor.ID,
		)
	}
	query += "ORDER BY metadata.title_initial,metadata.title COLLATE NOCASE,game.id LIMIT ?"
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
			&game.TitleInitial,
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
			&game.Favorited,
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
	featuredGames, err := queryFeaturedGames(ctx, transaction, profileID, platformID)
	if err != nil {
		return GamePage{}, err
	}
	platform.FeaturedGames = featuredGames
	games, err := queryGames(ctx, transaction, profileID, platformID, limit, cursor)
	if err != nil {
		return GamePage{}, err
	}
	var next *GameCursor
	if len(games) > limit {
		last := games[limit-1]
		next = &GameCursor{TitleInitial: last.TitleInitial, Title: last.Title, ID: last.ID}
		games = games[:limit]
	}
	if err := transaction.Commit(); err != nil {
		return GamePage{}, fmt.Errorf("immersive: commit read transaction: %w", err)
	}
	return GamePage{Platform: platform, Items: games, NextCursor: next}, nil
}
