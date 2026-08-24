package immersive

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func libraryCondition(kind, profileID, folderID string) (string, []any, error) {
	switch kind {
	case LibraryAll:
		if folderID != "" {
			return "", nil, ErrFavoriteFolderNotFound
		}
		return "1=1", nil, nil
	case LibraryRecent:
		if folderID != "" {
			return "", nil, ErrFavoriteFolderNotFound
		}
		return "profile_play.last_played_at_ms IS NOT NULL", nil, nil
	case LibraryFavorites:
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
	case LibrarySaves:
		if folderID != "" {
			return "", nil, ErrFavoriteFolderNotFound
		}
		return `EXISTS(
  SELECT 1 FROM save_states save
  WHERE save.profile_id=? AND save.game_id=game.id AND save.deleted_at_ms IS NULL
)`, []any{profileID}, nil
	default:
		return "", nil, ErrLibraryNotFound
	}
}

func libraryName(kind string) string {
	switch kind {
	case LibraryAll:
		return "全部游戏"
	case LibraryRecent:
		return "最近游玩"
	case LibraryFavorites:
		return "收藏游戏"
	case LibrarySaves:
		return "我的存档"
	default:
		return ""
	}
}

func queryLibrarySummary(
	ctx context.Context,
	database querier,
	profileID, kind, folderID string,
) (Destination, error) {
	condition, conditionArguments, err := libraryCondition(kind, profileID, folderID)
	if err != nil {
		return Destination{}, err
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
	var result Destination
	var lastPlayedAtMS sql.NullInt64
	if err := database.QueryRowContext(ctx, query, arguments...).Scan(
		&result.GameCount,
		&lastPlayedAtMS,
	); err != nil {
		return Destination{}, fmt.Errorf("immersive: query %s library summary: %w", kind, err)
	}
	result.ID = kind
	result.Kind = kind
	result.Name = libraryName(kind)
	result.LastPlayedAtMS = nullableInt64Pointer(lastPlayedAtMS)
	return result, nil
}

func queryLibraryFeaturedGames(
	ctx context.Context,
	database querier,
	profileID, kind, folderID string,
) ([]FeaturedGame, error) {
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
        AND asset.metadata_revision_id=game.current_metadata_revision_id
        AND asset.kind='COVER' AND asset.ordinal=0
        LIMIT 1),
       profile_play.last_played_at_ms
FROM games game
JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
LEFT JOIN profile_play ON profile_play.game_id=game.id
WHERE game.status='PUBLISHED' AND instance.enabled=1 AND (` + condition + `)
ORDER BY CASE WHEN profile_play.last_played_at_ms IS NULL THEN 1 ELSE 0 END,
         profile_play.last_played_at_ms DESC,
         game.created_at_ms DESC,
         game.id DESC
LIMIT 3`
	arguments := append([]any{profileID}, conditionArguments...)
	rows, err := database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("immersive: query %s featured games: %w", kind, err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]FeaturedGame, 0, 3)
	for rows.Next() {
		var game FeaturedGame
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

func (service *Service) Destinations(ctx context.Context, profileID string) ([]Destination, error) {
	transaction, err := service.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("immersive: begin destination transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	destinations := make([]Destination, 0, 4)
	for _, kind := range []string{LibraryAll, LibraryRecent, LibraryFavorites, LibrarySaves} {
		destination, queryErr := queryLibrarySummary(ctx, transaction, profileID, kind, "")
		if queryErr != nil {
			return nil, queryErr
		}
		destination.FeaturedGames, queryErr = queryLibraryFeaturedGames(
			ctx,
			transaction,
			profileID,
			kind,
			"",
		)
		if queryErr != nil {
			return nil, queryErr
		}
		destinations = append(destinations, destination)
	}
	platforms, err := queryPlatforms(ctx, transaction, profileID)
	if err != nil {
		return nil, err
	}
	featuredGames, err := queryFeaturedGames(ctx, transaction, profileID, "")
	if err != nil {
		return nil, err
	}
	attachFeaturedGames(platforms, featuredGames)
	for _, platform := range platforms {
		destinations = append(destinations, Destination{
			ID:             platform.ID,
			Kind:           "platform",
			Name:           platform.Name,
			GameCount:      platform.GameCount,
			LastPlayedAtMS: platform.LastPlayedAtMS,
			FeaturedGames:  platform.FeaturedGames,
		})
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("immersive: commit destination transaction: %w", err)
	}
	return destinations, nil
}

func queryFavoriteFolder(
	ctx context.Context,
	database querier,
	profileID, folderID string,
) (*FavoriteFolder, error) {
	if folderID == "" {
		return nil, ErrFavoriteFolderNotFound
	}
	var folder FavoriteFolder
	err := database.QueryRowContext(ctx, `
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
		return nil, ErrFavoriteFolderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("immersive: query favorite folder: %w", err)
	}
	return &folder, nil
}

func queryFavoriteFolders(
	ctx context.Context,
	database querier,
	profileID string,
) ([]FavoriteFolder, error) {
	rows, err := database.QueryContext(ctx, `
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
	result := make([]FavoriteFolder, 0)
	for rows.Next() {
		var folder FavoriteFolder
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

func scanLibraryGame(rows *sql.Rows) (Game, error) {
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
		return Game{}, fmt.Errorf("scan library game row: %w", err)
	}
	game.ReleaseYear = nullableInt64Pointer(releaseYear)
	game.CoverAssetID = nullableStringPointer(coverAssetID)
	game.VideoAssetID = nullableStringPointer(videoAssetID)
	game.LastPlayedAtMS = nullableInt64Pointer(lastPlayedAtMS)
	game.SaveStates = make([]SaveState, 0)
	return game, nil
}

func queryLibraryGames(
	ctx context.Context,
	database querier,
	profileID, kind, folderID string,
	limit int,
	cursor *GameCursor,
) ([]Game, error) {
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
        WHERE asset.game_id=game.id AND asset.metadata_revision_id=game.current_metadata_revision_id
        AND asset.kind='COVER' AND asset.ordinal=0 LIMIT 1),
       (SELECT asset.id FROM game_assets asset
        WHERE asset.game_id=game.id AND asset.metadata_revision_id=game.current_metadata_revision_id
        AND asset.kind='VIDEO' AND asset.ordinal=0 LIMIT 1),
       profile_play.last_played_at_ms,
       EXISTS(SELECT 1 FROM favorite_games favorite
              WHERE favorite.profile_id=? AND favorite.game_id=game.id)
FROM games game
JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN cores core ON core.id=instance.default_core_id
LEFT JOIN profile_play ON profile_play.game_id=game.id
WHERE game.status='PUBLISHED' AND instance.enabled=1 AND (` + condition + ")"
	arguments := append([]any{profileID, profileID}, conditionArguments...)
	if cursor != nil {
		if kind == LibraryRecent {
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
	if kind == LibraryRecent {
		query += " ORDER BY profile_play.last_played_at_ms DESC,game.id DESC LIMIT ?"
	} else {
		query += " ORDER BY metadata.title_initial,metadata.title COLLATE NOCASE,game.id LIMIT ?"
	}
	arguments = append(arguments, limit+1)
	rows, err := database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("immersive: query %s library games: %w", kind, err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]Game, 0, limit+1)
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

func attachSaveStates(
	ctx context.Context,
	database querier,
	profileID string,
	games []Game,
) error {
	if len(games) == 0 {
		return nil
	}
	arguments := make([]any, 0, len(games)+1)
	arguments = append(arguments, profileID)
	gameIndexes := make(map[string]int, len(games))
	for index := range games {
		arguments = append(arguments, games[index].ID)
		gameIndexes[games[index].ID] = index
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(games)), ",")
	rows, err := database.QueryContext(ctx, `
SELECT save.game_id,save.id,save.name,save.created_at_ms,save.disc_index
FROM save_states save
WHERE save.profile_id=? AND save.deleted_at_ms IS NULL
AND save.game_id IN (`+placeholders+`)
ORDER BY save.game_id,save.created_at_ms DESC,save.id DESC
`, arguments...)
	if err != nil {
		return fmt.Errorf("immersive: query save states: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var gameID string
		var save SaveState
		var discIndex sql.NullInt64
		if err := rows.Scan(&gameID, &save.ID, &save.Name, &save.CreatedAtMS, &discIndex); err != nil {
			return fmt.Errorf("immersive: scan save state: %w", err)
		}
		save.DiscIndex = nullableInt64Pointer(discIndex)
		if index, found := gameIndexes[gameID]; found {
			games[index].SaveStates = append(games[index].SaveStates, save)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("immersive: iterate save states: %w", err)
	}
	return nil
}

func resolveLibraryFolder(
	ctx context.Context,
	database querier,
	profileID, kind, folderID string,
) (FavoriteFolder, bool, error) {
	if folderID == "" {
		return FavoriteFolder{}, false, nil
	}
	if kind != LibraryFavorites {
		return FavoriteFolder{}, false, ErrFavoriteFolderNotFound
	}
	folder, err := queryFavoriteFolder(ctx, database, profileID, folderID)
	if err != nil {
		return FavoriteFolder{}, false, err
	}
	return *folder, true, nil
}

func libraryPageItems(games []Game, limit int, kind string) ([]Game, *GameCursor) {
	if len(games) <= limit {
		return games, nil
	}
	last := games[limit-1]
	next := &GameCursor{
		TitleInitial: last.TitleInitial,
		Title:        last.Title,
		ID:           last.ID,
	}
	if kind == LibraryRecent {
		next.LastPlayedAtMS = last.LastPlayedAtMS
	}
	return games[:limit], next
}

func (service *Service) LibraryGames(
	ctx context.Context,
	profileID, kind, folderID string,
	limit int,
	cursor *GameCursor,
) (LibraryPage, error) {
	transaction, err := service.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return LibraryPage{}, fmt.Errorf("immersive: begin library transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	if !ValidLibraryKind(kind) {
		return LibraryPage{}, ErrLibraryNotFound
	}
	resolvedFolder, hasFolder, err := resolveLibraryFolder(ctx, transaction, profileID, kind, folderID)
	if err != nil {
		return LibraryPage{}, err
	}
	var folder *FavoriteFolder
	if hasFolder {
		folder = &resolvedFolder
	}
	library, err := queryLibrarySummary(ctx, transaction, profileID, kind, folderID)
	if err != nil {
		return LibraryPage{}, err
	}
	library.FeaturedGames, err = queryLibraryFeaturedGames(ctx, transaction, profileID, kind, folderID)
	if err != nil {
		return LibraryPage{}, err
	}
	folders := make([]FavoriteFolder, 0)
	if kind == LibraryFavorites && folderID == "" {
		folders, err = queryFavoriteFolders(ctx, transaction, profileID)
		if err != nil {
			return LibraryPage{}, err
		}
	}
	games, err := queryLibraryGames(ctx, transaction, profileID, kind, folderID, limit, cursor)
	if err != nil {
		return LibraryPage{}, err
	}
	games, next := libraryPageItems(games, limit, kind)
	if kind == LibrarySaves {
		if err := attachSaveStates(ctx, transaction, profileID, games); err != nil {
			return LibraryPage{}, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return LibraryPage{}, fmt.Errorf("immersive: commit library transaction: %w", err)
	}
	return LibraryPage{
		Library: library, Folder: folder, Folders: folders, Items: games, NextCursor: next,
	}, nil
}
