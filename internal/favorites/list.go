package favorites

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"retrom/internal/tagging"
)

func validateListOptions(options ListOptions) (ListOptions, error) {
	if options.Scope == "" {
		options.Scope = ScopeAll
	}
	if options.Sort == "" {
		options.Sort = SortFavoritedDesc
	}
	if options.Limit == 0 {
		options.Limit = 50
	}
	if options.Limit < 1 || options.Limit > 100 || utf8.RuneCountInString(strings.TrimSpace(options.Query)) > 200 {
		return ListOptions{}, ErrInvalid
	}
	options.Query = canonicalSearch(options.Query)
	switch options.Scope {
	case ScopeAll, ScopeUncategorized:
		if options.FolderID != "" {
			return ListOptions{}, ErrInvalid
		}
	case ScopeFolder:
		if !ValidID(options.FolderID) {
			return ListOptions{}, ErrInvalid
		}
	default:
		return ListOptions{}, ErrInvalid
	}
	switch options.Sort {
	case SortFavoritedDesc, SortRecentlyPlayed, SortTitleAsc, SortReleaseYearDesc:
	default:
		return ListOptions{}, ErrInvalidFavoriteListSort
	}
	return options, nil
}

func querySummary(ctx context.Context, transaction *sql.Tx, profileID string) (Summary, error) {
	var result Summary
	err := transaction.QueryRowContext(ctx, `
SELECT
  count(*),
  count(CASE WHEN NOT EXISTS(
    SELECT 1 FROM favorite_folder_games membership
    WHERE membership.profile_id=favorite.profile_id
    AND membership.game_id=favorite.game_id
  ) THEN 1 END),
  (SELECT count(*) FROM favorite_folders folder WHERE folder.profile_id=?)
FROM favorite_games favorite
JOIN games game ON game.id=favorite.game_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
WHERE favorite.profile_id=?
AND game.status='PUBLISHED'
AND instance.enabled=1
`, profileID, profileID).Scan(
		&result.FavoriteCount, &result.UncategorizedCount, &result.FolderCount,
	)
	if err != nil {
		return Summary{}, fmt.Errorf("favorites: query summary: %w", err)
	}
	return result, nil
}

func queryFolders(ctx context.Context, transaction *sql.Tx, profileID string) ([]Folder, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT folder.id,folder.name,folder.version,folder.created_at_ms,folder.updated_at_ms,
       count(CASE WHEN game.status='PUBLISHED' AND instance.enabled=1 THEN 1 END)
FROM favorite_folders folder
LEFT JOIN favorite_folder_games membership
  ON membership.profile_id=folder.profile_id AND membership.folder_id=folder.id
LEFT JOIN games game ON game.id=membership.game_id
LEFT JOIN platform_instances instance ON instance.id=game.platform_instance_id
WHERE folder.profile_id=?
GROUP BY folder.id,folder.name,folder.version,folder.created_at_ms,folder.updated_at_ms
ORDER BY folder.created_at_ms,folder.id
`, profileID)
	if err != nil {
		return nil, fmt.Errorf("favorites: query folders: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]Folder, 0)
	for rows.Next() {
		var folder Folder
		if err := rows.Scan(
			&folder.FolderID, &folder.Name, &folder.Version, &folder.CreatedAtMS, &folder.UpdatedAtMS,
			&folder.VisibleGameCount,
		); err != nil {
			return nil, fmt.Errorf("favorites: scan folder: %w", err)
		}
		result = append(result, folder)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("favorites: iterate folders: %w", err)
	}
	return result, nil
}

func queryPlatforms(
	ctx context.Context,
	transaction *sql.Tx,
	profileID, scope, folderID string,
) ([]PlatformSummary, error) {
	query := `
SELECT platform.id,platform.name,count(*)
FROM favorite_games favorite
JOIN games game ON game.id=favorite.game_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE favorite.profile_id=?
AND game.status='PUBLISHED'
AND instance.enabled=1
AND (
  ?='ALL'
  OR (?='UNCATEGORIZED' AND NOT EXISTS(
    SELECT 1 FROM favorite_folder_games uncategorized
    WHERE uncategorized.profile_id=favorite.profile_id
    AND uncategorized.game_id=favorite.game_id
  ))
  OR (?='FOLDER' AND EXISTS(
    SELECT 1 FROM favorite_folder_games scoped
    WHERE scoped.profile_id=favorite.profile_id
    AND scoped.game_id=favorite.game_id
    AND scoped.folder_id=?
  ))
)
GROUP BY platform.id,platform.name
ORDER BY platform.name,platform.id`
	rows, err := transaction.QueryContext(ctx, query, profileID, scope, scope, scope, folderID)
	if err != nil {
		return nil, fmt.Errorf("favorites: query platforms: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]PlatformSummary, 0)
	for rows.Next() {
		var platform PlatformSummary
		if err := rows.Scan(&platform.ID, &platform.Name, &platform.Count); err != nil {
			return nil, fmt.Errorf("favorites: scan platform: %w", err)
		}
		result = append(result, platform)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("favorites: iterate platforms: %w", err)
	}
	return result, nil
}

func queryTotal(
	ctx context.Context,
	transaction *sql.Tx,
	profileID string,
	options ListOptions,
) (int64, error) {
	query := `
SELECT count(*)
FROM favorite_games favorite
JOIN games game ON game.id=favorite.game_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE favorite.profile_id=?
AND game.status='PUBLISHED'
AND instance.enabled=1
AND (
  ?='ALL'
  OR (?='UNCATEGORIZED' AND NOT EXISTS(
    SELECT 1 FROM favorite_folder_games uncategorized
    WHERE uncategorized.profile_id=favorite.profile_id
    AND uncategorized.game_id=favorite.game_id
  ))
  OR (?='FOLDER' AND EXISTS(
    SELECT 1 FROM favorite_folder_games scoped
    WHERE scoped.profile_id=favorite.profile_id
    AND scoped.game_id=favorite.game_id
    AND scoped.folder_id=?
  ))
)
AND (?='' OR instr(game.search_text,?)>0 OR EXISTS(
  SELECT 1 FROM game_tags relation JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
  WHERE relation.game_id=game.id AND instr(tag.search_text,?)>0
))
AND (?='' OR platform.id=?)`
	var count int64
	if err := transaction.QueryRowContext(
		ctx,
		query,
		profileID,
		options.Scope,
		options.Scope,
		options.Scope,
		options.FolderID,
		options.Query,
		options.Query,
		options.Query,
		options.PlatformID,
		options.PlatformID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("favorites: query total: %w", err)
	}
	return count, nil
}

func parseCursorInt(values []string, index int) (int64, error) {
	if index >= len(values) {
		return 0, ErrInvalidCursor
	}
	value, err := strconv.ParseInt(values[index], 10, 64)
	if err != nil {
		return 0, ErrInvalidCursor
	}
	return value, nil
}

const favoriteItemCandidatesSQL = `
WITH candidates AS (
  SELECT game.id AS game_id,metadata.title AS title,
         platform.id AS platform_id,platform.name AS platform_name,
         instance.id AS instance_id,instance.name AS instance_name,
         core.id AS core_id,core.name AS core_name,
         (SELECT asset.id FROM game_assets asset
          WHERE asset.game_id=game.id
          AND asset.metadata_revision_id=game.current_metadata_revision_id
          AND asset.kind='COVER'
          ORDER BY asset.ordinal,asset.id LIMIT 1) AS cover_asset_id,
         metadata.release_year AS release_year,
         game.created_at_ms AS created_at_ms,
         (SELECT max(play.started_at_ms) FROM play_sessions play
          WHERE play.profile_id=favorite.profile_id AND play.game_id=game.id) AS last_played_at_ms,
         favorite.created_at_ms AS favorited_at_ms,
         CASE WHEN EXISTS(SELECT 1 FROM play_sessions play
                          WHERE play.profile_id=favorite.profile_id AND play.game_id=game.id)
              THEN 0 ELSE 1 END AS played_rank,
         COALESCE((SELECT max(play.started_at_ms) FROM play_sessions play
                   WHERE play.profile_id=favorite.profile_id AND play.game_id=game.id),-1) AS last_played_key,
         CASE WHEN metadata.release_year IS NULL THEN 1 ELSE 0 END AS year_rank,
         COALESCE(metadata.release_year,-1) AS release_year_key
  FROM favorite_games favorite
  JOIN games game ON game.id=favorite.game_id
  JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
  JOIN platform_instances instance ON instance.id=game.platform_instance_id
  JOIN platforms platform ON platform.id=instance.platform_id
  JOIN cores core ON core.id=instance.default_core_id
  WHERE favorite.profile_id=?
  AND game.status='PUBLISHED'
  AND instance.enabled=1
  AND (
    ?='ALL'
    OR (?='UNCATEGORIZED' AND NOT EXISTS(
      SELECT 1 FROM favorite_folder_games uncategorized
      WHERE uncategorized.profile_id=favorite.profile_id
      AND uncategorized.game_id=favorite.game_id
    ))
    OR (?='FOLDER' AND EXISTS(
      SELECT 1 FROM favorite_folder_games scoped
      WHERE scoped.profile_id=favorite.profile_id
      AND scoped.game_id=favorite.game_id
      AND scoped.folder_id=?
    ))
  )
  AND (?='' OR instr(game.search_text,?)>0 OR EXISTS(
    SELECT 1 FROM game_tags relation JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
    WHERE relation.game_id=game.id AND instr(tag.search_text,?)>0
  ))
  AND (?='' OR platform.id=?)
)
`

const favoriteItemProjectionSQL = `
SELECT game_id,title,platform_id,platform_name,instance_id,instance_name,core_id,core_name,
       cover_asset_id,release_year,created_at_ms,last_played_at_ms,favorited_at_ms
FROM candidates
WHERE 1=1
`

const favoriteItemsFavoritedSQL = favoriteItemCandidatesSQL + favoriteItemProjectionSQL + `
AND (?=0 OR (favorited_at_ms<? OR (favorited_at_ms=? AND game_id<?)))
ORDER BY favorited_at_ms DESC,game_id DESC
LIMIT ?`

const favoriteItemsTitleSQL = favoriteItemCandidatesSQL + favoriteItemProjectionSQL + `
AND (?=0 OR (title>? OR (title=? AND game_id>?)))
ORDER BY title,game_id
LIMIT ?`

const favoriteItemsRecentlyPlayedSQL = favoriteItemCandidatesSQL + favoriteItemProjectionSQL + `
AND (?=0 OR (played_rank>? OR (played_rank=? AND (
  last_played_key<? OR (last_played_key=? AND (title>? OR (title=? AND game_id>?)))
))))
ORDER BY played_rank,last_played_key DESC,title,game_id
LIMIT ?`

const favoriteItemsReleaseYearSQL = favoriteItemCandidatesSQL + favoriteItemProjectionSQL + `
AND (?=0 OR (year_rank>? OR (year_rank=? AND (
  release_year_key<? OR (release_year_key=? AND (title>? OR (title=? AND game_id>?)))
))))
ORDER BY year_rank,release_year_key DESC,title,game_id
LIMIT ?`

func favoritedCursorArguments(cursor *PageCursor) ([]any, error) {
	if cursor == nil {
		return []any{0, nil, nil, nil}, nil
	}
	if !ValidID(cursor.ID) || len(cursor.SortValues) != 1 {
		return nil, ErrInvalidCursor
	}
	favorited, err := parseCursorInt(cursor.SortValues, 0)
	if err != nil {
		return nil, err
	}
	return []any{1, favorited, favorited, cursor.ID}, nil
}

func titleCursorArguments(cursor *PageCursor) ([]any, error) {
	if cursor == nil {
		return []any{0, nil, nil, nil}, nil
	}
	if !ValidID(cursor.ID) || len(cursor.SortValues) != 1 {
		return nil, ErrInvalidCursor
	}
	title := cursor.SortValues[0]
	return []any{1, title, title, cursor.ID}, nil
}

func compoundCursorArguments(cursor *PageCursor) ([]any, error) {
	if cursor == nil {
		return []any{0, nil, nil, nil, nil, nil, nil, nil}, nil
	}
	if !ValidID(cursor.ID) || len(cursor.SortValues) != 3 {
		return nil, ErrInvalidCursor
	}
	rank, err := parseCursorInt(cursor.SortValues, 0)
	if err != nil {
		return nil, err
	}
	value, err := parseCursorInt(cursor.SortValues, 1)
	if err != nil {
		return nil, err
	}
	title := cursor.SortValues[2]
	return []any{1, rank, rank, value, value, title, title, cursor.ID}, nil
}

func favoriteItemsQuery(options ListOptions) (string, []any, error) {
	switch options.Sort {
	case SortFavoritedDesc:
		arguments, err := favoritedCursorArguments(options.Cursor)
		return favoriteItemsFavoritedSQL, arguments, err
	case SortRecentlyPlayed:
		arguments, err := compoundCursorArguments(options.Cursor)
		return favoriteItemsRecentlyPlayedSQL, arguments, err
	case SortTitleAsc:
		arguments, err := titleCursorArguments(options.Cursor)
		return favoriteItemsTitleSQL, arguments, err
	case SortReleaseYearDesc:
		arguments, err := compoundCursorArguments(options.Cursor)
		return favoriteItemsReleaseYearSQL, arguments, err
	default:
		return "", nil, ErrInvalidFavoriteListSort
	}
}

func scanFavoriteGame(rows *sql.Rows) (GameItem, error) {
	var item GameItem
	var platformID, platformName, instanceID, instanceName, coreID, coreName string
	var coverAssetID sql.NullString
	var releaseYear, lastPlayed sql.NullInt64
	if err := rows.Scan(
		&item.GameID, &item.Title, &platformID, &platformName, &instanceID, &instanceName,
		&coreID, &coreName, &coverAssetID, &releaseYear, &item.CreatedAtMS, &lastPlayed,
		&item.Favorite.FavoritedAtMS,
	); err != nil {
		return GameItem{}, fmt.Errorf("favorites: scan game: %w", err)
	}
	item.Platform = NamedResource{ID: platformID, Name: platformName}
	item.PlatformInstance = NamedResource{ID: instanceID, Name: instanceName}
	item.DefaultCore = NamedResource{ID: coreID, Name: coreName}
	if coverAssetID.Valid {
		value := "/content/assets/" + coverAssetID.String
		item.CoverURL = &value
	}
	if releaseYear.Valid {
		value := releaseYear.Int64
		item.ReleaseYear = &value
	}
	if lastPlayed.Valid {
		value := lastPlayed.Int64
		item.LastPlayedAtMS = &value
	}
	item.Favorite.FolderIDs = []string{}
	return item, nil
}

func queryItems(
	ctx context.Context,
	transaction *sql.Tx,
	profileID string,
	options ListOptions,
) ([]GameItem, error) {
	query, cursorArguments, err := favoriteItemsQuery(options)
	if err != nil {
		return nil, err
	}
	arguments := make([]any, 0, 10+len(cursorArguments)+1)
	arguments = append(arguments,
		profileID,
		options.Scope,
		options.Scope,
		options.Scope,
		options.FolderID,
		options.Query,
		options.Query,
		options.Query,
		options.PlatformID,
		options.PlatformID,
	)
	arguments = append(arguments, cursorArguments...)
	arguments = append(arguments, options.Limit+1)
	rows, err := transaction.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("favorites: query items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]GameItem, 0, options.Limit+1)
	for rows.Next() {
		item, err := scanFavoriteGame(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("favorites: iterate items: %w", err)
	}
	return items, nil
}

func populateMemberships(
	ctx context.Context,
	transaction *sql.Tx,
	profileID string,
	items []GameItem,
) error {
	if len(items) == 0 {
		return nil
	}
	gameIDs := make([]string, len(items))
	byGame := make(map[string]*GameItem, len(items))
	for index := range items {
		gameIDs[index] = items[index].GameID
		byGame[items[index].GameID] = &items[index]
	}
	query := `
SELECT membership.game_id,membership.folder_id
FROM favorite_folder_games membership
JOIN favorite_folders folder
  ON folder.profile_id=membership.profile_id AND folder.id=membership.folder_id
WHERE membership.profile_id=?
AND membership.game_id IN (SELECT value FROM json_each(?))
ORDER BY membership.game_id,folder.created_at_ms,folder.id`
	rows, err := transaction.QueryContext(ctx, query, profileID, encodedStringList(gameIDs))
	if err != nil {
		return fmt.Errorf("favorites: query page memberships: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var gameID, folderID string
		if err := rows.Scan(&gameID, &folderID); err != nil {
			return fmt.Errorf("favorites: scan page membership: %w", err)
		}
		if item := byGame[gameID]; item != nil {
			item.Favorite.FolderIDs = append(item.Favorite.FolderIDs, folderID)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("favorites: iterate page memberships: %w", err)
	}
	return nil
}

func populateTags(ctx context.Context, transaction *sql.Tx, items []GameItem) error {
	if len(items) == 0 {
		return nil
	}
	gameIDs := make([]string, len(items))
	byGame := make(map[string]*GameItem, len(items))
	for index := range items {
		items[index].Tags = []tagging.Reference{}
		gameIDs[index] = items[index].GameID
		byGame[items[index].GameID] = &items[index]
	}
	rows, err := transaction.QueryContext(ctx, `
SELECT relation.game_id,tag.id,tag.name
FROM game_tags relation JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE relation.game_id IN (SELECT value FROM json_each(?))
ORDER BY relation.game_id,tag.name_key,tag.id
`, encodedStringList(gameIDs))
	if err != nil {
		return fmt.Errorf("favorites: query page tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var gameID string
		var reference tagging.Reference
		if err := rows.Scan(&gameID, &reference.TagID, &reference.Name); err != nil {
			return fmt.Errorf("favorites: scan page tag: %w", err)
		}
		if item := byGame[gameID]; item != nil {
			item.Tags = append(item.Tags, reference)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("favorites: iterate page tags: %w", err)
	}
	return nil
}

func itemCursor(item GameItem, sortCode string) *PageCursor {
	switch sortCode {
	case SortRecentlyPlayed:
		rank, last := int64(1), int64(-1)
		if item.LastPlayedAtMS != nil {
			rank, last = 0, *item.LastPlayedAtMS
		}
		return &PageCursor{
			SortValues: []string{strconv.FormatInt(rank, 10), strconv.FormatInt(last, 10), item.Title},
			ID:         item.GameID,
		}
	case SortTitleAsc:
		return &PageCursor{SortValues: []string{item.Title}, ID: item.GameID}
	case SortReleaseYearDesc:
		rank, year := int64(1), int64(-1)
		if item.ReleaseYear != nil {
			rank, year = 0, *item.ReleaseYear
		}
		return &PageCursor{
			SortValues: []string{strconv.FormatInt(rank, 10), strconv.FormatInt(year, 10), item.Title},
			ID:         item.GameID,
		}
	default:
		return &PageCursor{
			SortValues: []string{strconv.FormatInt(item.Favorite.FavoritedAtMS, 10)},
			ID:         item.GameID,
		}
	}
}

func (service *Service) List(
	ctx context.Context,
	principal Principal,
	requested ListOptions,
) (ListResult, error) {
	options, err := validateListOptions(requested)
	if err != nil {
		return ListResult{}, err
	}
	transaction, err := service.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ListResult{}, fmt.Errorf("favorites: begin read transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	if options.Scope == ScopeFolder {
		var found int
		err := transaction.QueryRowContext(ctx, `
SELECT 1 FROM favorite_folders WHERE profile_id=? AND id=?
`, principal.ProfileID, options.FolderID).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return ListResult{}, ErrFolderNotFound
		}
		if err != nil {
			return ListResult{}, fmt.Errorf("favorites: validate list folder: %w", err)
		}
	}
	summary, err := querySummary(ctx, transaction, principal.ProfileID)
	if err != nil {
		return ListResult{}, err
	}
	folders, err := queryFolders(ctx, transaction, principal.ProfileID)
	if err != nil {
		return ListResult{}, err
	}
	platforms, err := queryPlatforms(ctx, transaction, principal.ProfileID, options.Scope, options.FolderID)
	if err != nil {
		return ListResult{}, err
	}
	total, err := queryTotal(ctx, transaction, principal.ProfileID, options)
	if err != nil {
		return ListResult{}, err
	}
	items, err := queryItems(ctx, transaction, principal.ProfileID, options)
	if err != nil {
		return ListResult{}, err
	}
	var next *PageCursor
	if len(items) > options.Limit {
		items = items[:options.Limit]
		next = itemCursor(items[len(items)-1], options.Sort)
	}
	if err := populateMemberships(ctx, transaction, principal.ProfileID, items); err != nil {
		return ListResult{}, err
	}
	if err := populateTags(ctx, transaction, items); err != nil {
		return ListResult{}, err
	}
	if err := transaction.Commit(); err != nil {
		return ListResult{}, fmt.Errorf("favorites: commit read transaction: %w", err)
	}
	return ListResult{
		GeneratedAtMS: service.now().UnixMilli(), Summary: summary, Folders: folders,
		Platforms: platforms, TotalCount: total, Items: items, NextCursor: next,
	}, nil
}
