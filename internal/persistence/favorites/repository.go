package favorites

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"

	"retrom/internal/service/favorites"
)

type Repository struct{ database dbapi.DB }

func New(database dbapi.DB) *Repository { return &Repository{database: database} }
func (service *Repository) WithWrite(ctx context.Context, work func(favorites.WriteScope) error) error {
	err := dbapi.RetryTransaction(ctx, service.database, func(tx dbapi.Tx) error { return work(writeScope(tx)) })
	if err != nil {
		return fmt.Errorf("commit favorites transaction: %w", err)
	}
	return nil
}

func (service *Repository) Reference(
	ctx context.Context, profileID, gameID string,
) (*favorites.FavoriteReference, error) {
	state, exists, err := (gameRecords{service.database}).State(ctx, profileID, gameID)
	if err != nil || !exists {
		return nil, err
	}
	return &favorites.FavoriteReference{FavoritedAtMS: state.FavoritedAtMS, FolderIDs: state.FolderIDs}, nil
}

func (service *Repository) References(
	ctx context.Context,
	profileID string,
	gameIDs []string,
) (map[string]favorites.FavoriteReference, error) {
	result := make(map[string]favorites.FavoriteReference)
	if len(gameIDs) == 0 {
		return result, nil
	}
	query := `
SELECT game_id,created_at_ms
FROM favorite_games
WHERE profile_id=? AND game_id IN (SELECT value FROM jsonb_array_elements_text((?)::jsonb))`
	rows, err := service.database.QueryContext(ctx, query, profileID, encodedStringList(gameIDs))
	if err != nil {
		return nil, fmt.Errorf("favorites: query references: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var gameID string
		var createdAtMS int64
		if err := rows.Scan(&gameID, &createdAtMS); err != nil {
			return nil, fmt.Errorf("favorites: scan reference: %w", err)
		}
		result[gameID] = favorites.FavoriteReference{FavoritedAtMS: createdAtMS, FolderIDs: []string{}}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("favorites: iterate references: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("favorites: close references: %w", err)
	}
	if len(result) == 0 {
		return result, nil
	}
	membershipRows, err := service.database.QueryContext(ctx, `
SELECT membership.game_id,membership.folder_id
FROM favorite_folder_games membership
JOIN favorite_folders folder
  ON folder.profile_id=membership.profile_id AND folder.id=membership.folder_id
WHERE membership.profile_id=?
AND membership.game_id IN (SELECT value FROM jsonb_array_elements_text((?)::jsonb))
ORDER BY membership.game_id,folder.created_at_ms,folder.id
`, profileID, encodedStringList(gameIDs))
	if err != nil {
		return nil, fmt.Errorf("favorites: query reference memberships: %w", err)
	}
	defer func() { _ = membershipRows.Close() }()
	for membershipRows.Next() {
		var gameID, folderID string
		if err := membershipRows.Scan(&gameID, &folderID); err != nil {
			return nil, fmt.Errorf("favorites: scan reference membership: %w", err)
		}
		if reference, exists := result[gameID]; exists {
			reference.FolderIDs = append(reference.FolderIDs, folderID)
			result[gameID] = reference
		}
	}
	if err := membershipRows.Err(); err != nil {
		return nil, fmt.Errorf("favorites: iterate reference memberships: %w", err)
	}
	return result, nil
}
