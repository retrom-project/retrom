package favorites

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/model/favorites"
)

func encodedStringList(values []string) string {
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

func (records gameRecords) Visible(ctx context.Context, gameID string) (bool, error) {
	var found int
	err := records.database.QueryRowContext(ctx, `
SELECT 1
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE g.id=?
AND g.status='PUBLISHED'
AND pi.enabled=1
`, gameID).Scan(&found)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	default:
		return false, fmt.Errorf("favorites: check game visibility: %w", err)
	}
}

func (records gameRecords) RequireVisible(ctx context.Context, gameIDs []string) error {
	if len(gameIDs) == 0 {
		return nil
	}
	var count int
	query := `
SELECT count(*)
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE g.id IN (SELECT value FROM json_each(?))
AND g.status='PUBLISHED'
AND pi.enabled=1`
	if err := records.database.QueryRowContext(ctx, query, encodedStringList(gameIDs)).Scan(&count); err != nil {
		return fmt.Errorf("favorites: validate games: %w", err)
	}
	if count != len(gameIDs) {
		return favorites.ErrGameNotFound
	}
	return nil
}

func (records folderRecords) Require(ctx context.Context, profileID string, folderIDs []string) error {
	if len(folderIDs) == 0 {
		return nil
	}
	var count int
	query := `
SELECT count(*)
FROM favorite_folders
WHERE profile_id=? AND id IN (SELECT value FROM json_each(?))`
	if err := records.database.QueryRowContext(
		ctx, query, profileID, encodedStringList(folderIDs),
	).Scan(&count); err != nil {
		return fmt.Errorf("favorites: validate folders: %w", err)
	}
	if count != len(folderIDs) {
		return favorites.ErrFolderNotFound
	}
	return nil
}

func (records membershipRecords) FolderIDs(ctx context.Context, profileID, gameID string) ([]string, error) {
	rows, err := records.database.QueryContext(ctx, `
SELECT membership.folder_id
FROM favorite_folder_games membership
JOIN favorite_folders folder
  ON folder.profile_id=membership.profile_id AND folder.id=membership.folder_id
WHERE membership.profile_id=? AND membership.game_id=?
ORDER BY folder.created_at_ms,folder.id
`, profileID, gameID)
	if err != nil {
		return nil, fmt.Errorf("favorites: query game folders: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]string, 0)
	for rows.Next() {
		var folderID string
		if err := rows.Scan(&folderID); err != nil {
			return nil, fmt.Errorf("favorites: scan game folder: %w", err)
		}
		result = append(result, folderID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("favorites: iterate game folders: %w", err)
	}
	return result, nil
}

func (records gameRecords) State(ctx context.Context, profileID, gameID string) (favorites.State, bool, error) {
	var createdAtMS int64
	err := records.database.QueryRowContext(ctx, `
SELECT created_at_ms FROM favorite_games WHERE profile_id=? AND game_id=?
`, profileID, gameID).Scan(&createdAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return favorites.State{}, false, nil
	}
	if err != nil {
		return favorites.State{}, false, fmt.Errorf("favorites: read favorite: %w", err)
	}
	folderIDs, err := membershipRecords(records).FolderIDs(ctx, profileID, gameID)
	if err != nil {
		return favorites.State{}, false, err
	}
	return favorites.State{GameID: gameID, FavoritedAtMS: createdAtMS, FolderIDs: folderIDs}, true, nil
}

func (records gameRecords) Ensure(ctx context.Context, profileID, gameID string, now int64) error {
	if _, err := records.database.ExecContext(ctx, `
INSERT INTO favorite_games(profile_id,game_id,created_at_ms) VALUES(?,?,?)
ON CONFLICT(profile_id,game_id) DO NOTHING
`, profileID, gameID, now); err != nil {
		return fmt.Errorf("favorites: ensure favorite: %w", err)
	}
	return nil
}

func (records membershipRecords) Remove(ctx context.Context, profileID, folderID, gameID string) error {
	if _, err := records.database.ExecContext(ctx, `
DELETE FROM favorite_folder_games WHERE profile_id=? AND folder_id=? AND game_id=?
`, profileID, folderID, gameID); err != nil {
		return fmt.Errorf("favorites: remove membership: %w", err)
	}
	return nil
}

func (records membershipRecords) Add(
	ctx context.Context,
	profileID, folderID, gameID string,
	now int64,
) error {
	if _, err := records.database.ExecContext(ctx, `
INSERT INTO favorite_folder_games(profile_id,folder_id,game_id,created_at_ms) VALUES(?,?,?,?)
ON CONFLICT(profile_id,folder_id,game_id) DO NOTHING
`, profileID, folderID, gameID, now); err != nil {
		return fmt.Errorf("favorites: add membership: %w", err)
	}
	return nil
}

func (records folderRecords) Existing(
	ctx context.Context,
	profileID string,
	requested []string,
) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(requested))
	if len(requested) == 0 {
		return result, nil
	}
	rows, err := records.database.QueryContext(ctx, `
SELECT id
FROM favorite_folders
WHERE profile_id=? AND id IN (SELECT value FROM json_each(?))
`, profileID, encodedStringList(requested))
	if err != nil {
		return nil, fmt.Errorf("favorites: query restore folders: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var folderID string
		if err := rows.Scan(&folderID); err != nil {
			return nil, fmt.Errorf("favorites: scan restore folder: %w", err)
		}
		result[folderID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("favorites: iterate restore folders: %w", err)
	}
	return result, nil
}

func (records folderRecords) RequireAvailableName(
	ctx context.Context,
	profileID, nameKey, excludedFolderID string,
) error {
	var count int
	if err := records.database.QueryRowContext(ctx, `
SELECT count(*)
FROM favorite_folders
WHERE profile_id=? AND name_key=? AND (?='' OR id<>?)
`, profileID, nameKey, excludedFolderID, excludedFolderID).Scan(&count); err != nil {
		return fmt.Errorf("favorites: check folder name: %w", err)
	}
	if count > 0 {
		return favorites.ErrFolderNameConflict
	}
	return nil
}

func (records folderRecords) Get(ctx context.Context, profileID, folderID string) (favorites.Folder, error) {
	var folder favorites.Folder
	err := records.database.QueryRowContext(ctx, `
SELECT folder.id,folder.name,folder.version,folder.created_at_ms,folder.updated_at_ms,
       count(CASE WHEN game.status='DELETED' OR game.status='PUBLISHED' AND instance.enabled=1 THEN 1 END)
FROM favorite_folders folder
LEFT JOIN favorite_folder_games membership
  ON membership.profile_id=folder.profile_id AND membership.folder_id=folder.id
LEFT JOIN favorite_games favorite
  ON favorite.profile_id=membership.profile_id AND favorite.game_id=membership.game_id
LEFT JOIN games game ON game.id=favorite.game_id AND game.status IN ('PUBLISHED','DELETED')
LEFT JOIN platform_instances instance
  ON instance.id=game.platform_instance_id
WHERE folder.profile_id=? AND folder.id=?
GROUP BY folder.id,folder.name,folder.version,folder.created_at_ms,folder.updated_at_ms
`, profileID, folderID).Scan(
		&folder.FolderID, &folder.Name, &folder.Version, &folder.CreatedAtMS, &folder.UpdatedAtMS,
		&folder.VisibleGameCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return favorites.Folder{}, favorites.ErrFolderNotFound
	}
	if err != nil {
		return favorites.Folder{}, fmt.Errorf("favorites: read folder: %w", err)
	}
	return folder, nil
}
