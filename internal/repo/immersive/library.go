package immersive

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/model/immersive"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/storequery"
)

func libraryCondition(kind, profileID, folderID string) (string, []any, error) {
	switch kind {
	case immersive.LibraryAll:
		if folderID != "" {
			return "", nil, immersive.ErrFavoriteFolderNotFound
		}
		return "1=1", nil, nil
	case immersive.LibraryRecent:
		if folderID != "" {
			return "", nil, immersive.ErrFavoriteFolderNotFound
		}
		return "profile_play.last_played_at_ms IS NOT NULL", nil, nil
	case immersive.LibraryFavorites:
		condition := `EXISTS(
  SELECT 1 FROM favorite_games favorite
  WHERE favorite.profile_id=? AND favorite.game_id=game.id
)`
		arguments := []any{profileID}
		if folderID != "" {
			condition += ` AND EXISTS(
  SELECT 1 FROM favorite_folder_games membership
  WHERE membership.profile_id=? AND membership.folder_id=? AND membership.game_id=game.id
)`
			arguments = append(arguments, profileID, folderID)
		}
		return condition, arguments, nil
	case immersive.LibrarySaves:
		if folderID != "" {
			return "", nil, immersive.ErrFavoriteFolderNotFound
		}
		return `EXISTS(
  SELECT 1 FROM save_states save
  JOIN (` + storequery.SaveRuntimeCompatibility + `) compatibility
    ON compatibility.save_state_id=save.id AND compatibility.status='AVAILABLE'
  WHERE save.profile_id=? AND save.game_id=game.id AND save.deleted_at_ms IS NULL
)`, []any{profileID}, nil
	default:
		return "", nil, immersive.ErrLibraryNotFound
	}
}

func (records libraryRecords) Summary(
	ctx context.Context,
	profileID, kind, folderID string,
) (immersive.Destination, error) {
	condition, conditionArguments, err := libraryCondition(kind, profileID, folderID)
	if err != nil {
		return immersive.Destination{}, err
	}
	query := `
WITH profile_play AS (
  SELECT session.game_id,max(session.started_at_ms) AS last_played_at_ms
  FROM play_sessions session
  WHERE session.profile_id=?
  GROUP BY session.game_id
)
SELECT count(*),max(profile_play.last_played_at_ms)
FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
LEFT JOIN profile_play ON profile_play.game_id=game.id
WHERE game.status='PUBLISHED' AND instance.enabled=1 AND (` + condition + ")"
	arguments := append([]any{profileID}, conditionArguments...)
	var result immersive.Destination
	var lastPlayedAtMS sql.NullInt64
	if err := records.database.QueryRowContext(ctx, query, arguments...).Scan(
		&result.GameCount,
		&lastPlayedAtMS,
	); err != nil {
		return immersive.Destination{}, fmt.Errorf("immersive: query %s library summary: %w", kind, err)
	}
	result.ID = kind
	result.Kind = kind
	result.LastPlayedAtMS = nullableInt64Pointer(lastPlayedAtMS)
	return result, nil
}

func (records libraryRecords) Featured(
	ctx context.Context,
	profileID, kind, folderID string,
) ([]immersive.FeaturedGame, error) {
	condition, conditionArguments, err := libraryCondition(kind, profileID, folderID)
	if err != nil {
		return nil, err
	}
	query := `
WITH profile_play AS (
  SELECT session.game_id,max(session.started_at_ms) AS last_played_at_ms
  FROM play_sessions session
  WHERE session.profile_id=?
  GROUP BY session.game_id
)
SELECT game.id,
       metadata.title,
       (SELECT asset.id
        FROM game_assets asset
        WHERE asset.game_id=game.id
        AND asset.game_id=game.id
        AND asset.kind='COVER' AND asset.ordinal=0
        LIMIT 1),
       profile_play.last_played_at_ms
FROM games game
JOIN games metadata ON metadata.id=game.id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
LEFT JOIN profile_play ON profile_play.game_id=game.id
WHERE game.status='PUBLISHED' AND instance.enabled=1 AND (` + condition + `)
ORDER BY CASE WHEN profile_play.last_played_at_ms IS NULL THEN 1 ELSE 0 END,
         profile_play.last_played_at_ms DESC,
         game.created_at_ms DESC,
         game.id DESC
LIMIT 3`
	arguments := append([]any{profileID}, conditionArguments...)
	rows, err := records.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("immersive: query %s featured games: %w", kind, err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]immersive.FeaturedGame, 0, 3)
	for rows.Next() {
		var game immersive.FeaturedGame
		var coverAssetID sql.NullString
		var lastPlayedAtMS sql.NullInt64
		if err := rows.Scan(&game.ID, &game.Title, &coverAssetID, &lastPlayedAtMS); err != nil {
			return nil, fmt.Errorf("immersive: scan %s featured game: %w", kind, err)
		}
		game.PlatformID = kind
		game.CoverAssetID = nullableStringPointer(coverAssetID)
		game.LastPlayedAtMS = nullableInt64Pointer(lastPlayedAtMS)
		result = append(result, game)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("immersive: iterate %s featured games: %w", kind, err)
	}
	return result, nil
}

func (records libraryRecords) Folder(
	ctx context.Context,
	profileID, folderID string,
) (*immersive.FavoriteFolder, error) {
	if folderID == "" {
		return nil, immersive.ErrFavoriteFolderNotFound
	}
	var folder immersive.FavoriteFolder
	err := records.database.QueryRowContext(ctx, `
SELECT folder.id,folder.name,count(CASE WHEN game.status='PUBLISHED' AND instance.enabled=1 THEN 1 END)
FROM favorite_folders folder
LEFT JOIN favorite_folder_games membership
  ON membership.profile_id=folder.profile_id AND membership.folder_id=folder.id
LEFT JOIN games game ON game.id=membership.game_id
LEFT JOIN platform_instances instance ON instance.id=game.platform_instance_id
WHERE folder.profile_id=? AND folder.id=?
GROUP BY folder.id,folder.name
`, profileID, folderID).Scan(&folder.ID, &folder.Name, &folder.GameCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, immersive.ErrFavoriteFolderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("immersive: query favorite folder: %w", err)
	}
	return &folder, nil
}

func (records libraryRecords) Folders(
	ctx context.Context,
	profileID string,
) ([]immersive.FavoriteFolder, error) {
	rows, err := records.database.QueryContext(ctx, `
SELECT folder.id,folder.name,count(CASE WHEN game.status='PUBLISHED' AND instance.enabled=1 THEN 1 END)
FROM favorite_folders folder
LEFT JOIN favorite_folder_games membership
  ON membership.profile_id=folder.profile_id AND membership.folder_id=folder.id
LEFT JOIN games game ON game.id=membership.game_id
LEFT JOIN platform_instances instance ON instance.id=game.platform_instance_id
WHERE folder.profile_id=?
GROUP BY folder.id,folder.name,folder.created_at_ms
ORDER BY folder.created_at_ms,folder.id
`, profileID)
	if err != nil {
		return nil, fmt.Errorf("immersive: query favorite folders: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]immersive.FavoriteFolder, 0)
	for rows.Next() {
		var folder immersive.FavoriteFolder
		if err := rows.Scan(&folder.ID, &folder.Name, &folder.GameCount); err != nil {
			return nil, fmt.Errorf("immersive: scan favorite folder: %w", err)
		}
		result = append(result, folder)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("immersive: iterate favorite folders: %w", err)
	}
	return result, nil
}

func (records libraryRecords) Games(
	ctx context.Context,
	profileID, kind, folderID string,
	limit int,
	cursor *immersive.GameCursor,
) ([]immersive.Game, error) {
	condition, conditionArguments, err := libraryCondition(kind, profileID, folderID)
	if err != nil {
		return nil, err
	}
	query := `
WITH profile_play AS (
  SELECT session.game_id,max(session.started_at_ms) AS last_played_at_ms
  FROM play_sessions session
  WHERE session.profile_id=?
  GROUP BY session.game_id
)
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
       (SELECT asset.id FROM game_assets asset
        WHERE asset.game_id=game.id AND asset.game_id=game.id
        AND asset.kind='COVER' AND asset.ordinal=0 LIMIT 1),
       (SELECT asset.id FROM game_assets asset
        WHERE asset.game_id=game.id AND asset.game_id=game.id
        AND asset.kind='VIDEO' AND asset.ordinal=0 LIMIT 1),
       profile_play.last_played_at_ms,
       EXISTS(SELECT 1 FROM favorite_games favorite
              WHERE favorite.profile_id=? AND favorite.game_id=game.id)
FROM games game
JOIN games metadata ON metadata.id=game.id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN cores core ON core.id=instance.default_core_id
LEFT JOIN profile_play ON profile_play.game_id=game.id
WHERE game.status='PUBLISHED' AND instance.enabled=1 AND (` + condition + ")"
	arguments := append([]any{profileID, profileID}, conditionArguments...)
	if cursor != nil {
		if kind == immersive.LibraryRecent {
			query += ` AND (
 profile_play.last_played_at_ms<?
 OR (profile_play.last_played_at_ms=? AND game.id<?)
)`
			arguments = append(arguments, *cursor.LastPlayedAtMS, *cursor.LastPlayedAtMS, cursor.ID)
		} else {
			query += ` AND (
 metadata.title_initial>?
 OR (metadata.title_initial=? AND metadata.title COLLATE NOCASE>? COLLATE NOCASE)
 OR (metadata.title_initial=? AND metadata.title COLLATE NOCASE=? COLLATE NOCASE AND game.id>?)
)`
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
	}
	if kind == immersive.LibraryRecent {
		query += " ORDER BY profile_play.last_played_at_ms DESC,game.id DESC LIMIT ?"
	} else {
		query += " ORDER BY metadata.title_initial,metadata.title COLLATE NOCASE,game.id LIMIT ?"
	}
	arguments = append(arguments, limit+1)
	rows, err := records.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("immersive: query %s library games: %w", kind, err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]immersive.Game, 0, limit+1)
	for rows.Next() {
		game, scanErr := scanLibraryGame(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("immersive: scan %s library game: %w", kind, scanErr)
		}
		result = append(result, game)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("immersive: iterate %s library games: %w", kind, err)
	}
	return result, nil
}

func scanLibraryGame(rows dbexec.Scanner) (immersive.Game, error) {
	var game immersive.Game
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
		return immersive.Game{}, fmt.Errorf("scan library game row: %w", err)
	}
	game.ReleaseYear = nullableInt64Pointer(releaseYear)
	game.CoverAssetID = nullableStringPointer(coverAssetID)
	game.VideoAssetID = nullableStringPointer(videoAssetID)
	game.LastPlayedAtMS = nullableInt64Pointer(lastPlayedAtMS)
	game.SaveStates = make([]immersive.SaveState, 0)
	return game, nil
}
