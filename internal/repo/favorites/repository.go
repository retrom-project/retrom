package favorites

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/model/favorites"
	"retrom/internal/repo/dbexec"
)

type Repository struct{ database *sql.DB }

type idempotentWork func(dbexec.Executor) (int, map[string]string, any, error)

func New(database *sql.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) CommitFavorite(
	ctx context.Context, cmd favorites.FavoriteCommand,
) (favorites.State, error) {
	var state favorites.State
	err := dbexec.Immediate(ctx, repository.database, func(db dbexec.Executor) error {
		games := gameRecords{db}
		visible, err := games.Visible(ctx, cmd.GameID)
		if err != nil {
			return fmt.Errorf("favorites/Favorite: %w", err)
		}
		if !visible {
			return favorites.ErrGameNotFound
		}
		if err := games.Ensure(ctx, cmd.ProfileID, cmd.GameID, cmd.NowMS); err != nil {
			return fmt.Errorf("favorites: insert favorite: %w", err)
		}
		var exists bool
		state, exists, err = games.State(ctx, cmd.ProfileID, cmd.GameID)
		if err != nil {
			return fmt.Errorf("favorites/Favorite: %w", err)
		}
		if !exists {
			return favorites.ErrInvariant
		}
		return nil
	})
	return state, err
}

func (repository *Repository) CommitReplaceFolders(
	ctx context.Context, cmd favorites.ReplaceFoldersCommand,
) (favorites.State, error) {
	var state favorites.State
	err := dbexec.Immediate(ctx, repository.database, func(db dbexec.Executor) error {
		games := gameRecords{db}
		folders := folderRecords{db}
		memberships := membershipRecords{db}

		if err := games.RequireVisible(ctx, []string{cmd.GameID}); err != nil {
			return fmt.Errorf("favorites/ReplaceFolders: %w", err)
		}
		if err := folders.Require(ctx, cmd.ProfileID, cmd.FolderIDs); err != nil {
			return fmt.Errorf("favorites/ReplaceFolders: %w", err)
		}
		if err := games.Ensure(ctx, cmd.ProfileID, cmd.GameID, cmd.NowMS); err != nil {
			return fmt.Errorf("favorites/ReplaceFolders: %w", err)
		}
		current, err := memberships.FolderIDs(ctx, cmd.ProfileID, cmd.GameID)
		if err != nil {
			return fmt.Errorf("favorites/ReplaceFolders: %w", err)
		}
		if err := replaceMemberships(ctx, db, cmd.ProfileID, cmd.GameID, cmd.FolderIDs, current, cmd.NowMS); err != nil {
			return err
		}
		state, _, err = games.State(ctx, cmd.ProfileID, cmd.GameID)
		if err != nil {
			return fmt.Errorf("favorites/ReplaceFolders: %w", err)
		}
		return nil
	})
	return state, err
}

func replaceMemberships(
	ctx context.Context, db dbexec.Executor,
	profileID, gameID string,
	desired, current []string, now int64,
) error {
	memberships := membershipRecords{db}
	desiredSet := make(map[string]struct{}, len(desired))
	for _, folderID := range desired {
		desiredSet[folderID] = struct{}{}
	}
	currentSet := make(map[string]struct{}, len(current))
	for _, folderID := range current {
		currentSet[folderID] = struct{}{}
		if _, keep := desiredSet[folderID]; !keep {
			if err := memberships.Remove(ctx, profileID, folderID, gameID); err != nil {
				return fmt.Errorf("favorites/replaceMemberships: %w", err)
			}
		}
	}
	for _, folderID := range desired {
		if _, exists := currentSet[folderID]; !exists {
			if err := memberships.Add(ctx, profileID, folderID, gameID, now); err != nil {
				return fmt.Errorf("favorites/replaceMemberships: %w", err)
			}
		}
	}
	return nil
}

func (repository *Repository) commitIdempotent(
	ctx context.Context,
	envelope favorites.IdempotencyEnvelope,
	work idempotentWork,
) (favorites.IdempotentResponse, error) {
	var response favorites.IdempotentResponse
	err := dbexec.Immediate(ctx, repository.database, func(db dbexec.Executor) error {
		replayed, found, err := checkIdempotency(ctx, db, envelope)
		if err != nil {
			return err
		}
		if found {
			response = replayed
			return nil
		}
		status, headers, bodyValue, err := work(db)
		if err != nil {
			return err
		}
		body := []byte{}
		if bodyValue != nil {
			body, err = json.Marshal(bodyValue)
			if err != nil {
				return fmt.Errorf("favorites: encode idempotent response: %w", err)
			}
			body = append(body, '\n')
		}
		response = favorites.IdempotentResponse{Status: status, Headers: headers, Body: body}
		return saveIdempotencyRecord(ctx, db, envelope, response)
	})
	return response, err
}

func (repository *Repository) CommitOrganize(
	ctx context.Context, cmd favorites.OrganizeCommand,
) (favorites.IdempotentResponse, error) {
	return repository.commitIdempotent(ctx, cmd.Idempotency, func(db dbexec.Executor) (
		int, map[string]string, any, error,
	) {
		games := gameRecords{db}
		folders := folderRecords{db}
		memberships := membershipRecords{db}

		if err := games.RequireVisible(ctx, cmd.GameIDs); err != nil {
			return 0, nil, nil, fmt.Errorf("favorites/organizeWork: %w", err)
		}
		allFolders := append(append([]string{}, cmd.AddFolderIDs...), cmd.RemoveFolderIDs...)
		if err := folders.Require(ctx, cmd.ProfileID, allFolders); err != nil {
			return 0, nil, nil, fmt.Errorf("favorites/organizeWork: %w", err)
		}
		result := favorites.BatchResult{Items: make([]favorites.State, 0, len(cmd.GameIDs))}
		now := cmd.Idempotency.NowMS
		for _, gameID := range cmd.GameIDs {
			state, exists, err := organizeGame(
				ctx, db, games, memberships,
				cmd.ProfileID, gameID,
				cmd.AddFolderIDs, cmd.RemoveFolderIDs, now,
			)
			if err != nil {
				return 0, nil, nil, err
			}
			if exists {
				result.Items = append(result.Items, state)
			}
		}
		return 200, map[string]string{"Content-Type": "application/json; charset=utf-8"}, result, nil
	})
}

func organizeGame(
	ctx context.Context, _ dbexec.Executor,
	games gameRecords, memberships membershipRecords,
	profileID, gameID string,
	add, remove []string, now int64,
) (favorites.State, bool, error) {
	if len(add) > 0 {
		if err := games.Ensure(ctx, profileID, gameID, now); err != nil {
			return favorites.State{}, false, fmt.Errorf("favorites/organizeGame: %w", err)
		}
	}
	for _, folderID := range remove {
		if err := memberships.Remove(ctx, profileID, folderID, gameID); err != nil {
			return favorites.State{}, false, fmt.Errorf("favorites/organizeGame: %w", err)
		}
	}
	for _, folderID := range add {
		if err := memberships.Add(ctx, profileID, folderID, gameID, now); err != nil {
			return favorites.State{}, false, fmt.Errorf("favorites/organizeGame: %w", err)
		}
	}
	state, exists, err := games.State(ctx, profileID, gameID)
	return state, exists, err
}

func (repository *Repository) CommitUnfavorite(
	ctx context.Context, cmd favorites.UnfavoriteCommand,
) (favorites.IdempotentResponse, error) {
	return repository.commitIdempotent(ctx, cmd.Idempotency, func(db dbexec.Executor) (
		int, map[string]string, any, error,
	) {
		games := gameRecords{db}
		memberships := membershipRecords{db}

		result := favorites.UnfavoriteResult{Items: make([]favorites.UnfavoriteItem, 0, len(cmd.GameIDs))}
		for _, gameID := range cmd.GameIDs {
			_, exists, err := games.State(ctx, cmd.ProfileID, gameID)
			if err != nil {
				return 0, nil, nil, fmt.Errorf("favorites/Unfavorite: %w", err)
			}
			if !exists {
				continue
			}
			folderIDs, err := memberships.FolderIDs(ctx, cmd.ProfileID, gameID)
			if err != nil {
				return 0, nil, nil, fmt.Errorf("favorites/Unfavorite: %w", err)
			}
			result.Items = append(result.Items, favorites.UnfavoriteItem{GameID: gameID, FolderIDs: folderIDs})
			if err := games.Remove(ctx, cmd.ProfileID, gameID); err != nil {
				return 0, nil, nil, fmt.Errorf("favorites/Unfavorite: %w", err)
			}
		}
		return 200, map[string]string{"Content-Type": "application/json; charset=utf-8"}, result, nil
	})
}

func (repository *Repository) CommitRestore(
	ctx context.Context, cmd favorites.RestoreCommand,
) (favorites.IdempotentResponse, error) {
	return repository.commitIdempotent(ctx, cmd.Idempotency, func(db dbexec.Executor) (
		int, map[string]string, any, error,
	) {
		games := gameRecords{db}
		memberships := membershipRecords{db}
		folders := folderRecords{db}

		requestedFolders := restoreRequestedFolderIDs(cmd.Items)
		existingFolders, err := folders.Existing(ctx, cmd.ProfileID, requestedFolders)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("favorites/restoreWork: %w", err)
		}
		result := favorites.RestoreResult{
			RestoredGameIDs:  []string{},
			SkippedGameIDs:   []string{},
			SkippedFolderIDs: []string{},
		}
		skippedFolders := make(map[string]struct{})
		now := cmd.Idempotency.NowMS
		for _, item := range cmd.Items {
			restored, err := restoreItem(ctx, games, memberships, cmd.ProfileID, item, existingFolders, skippedFolders, now)
			if err != nil {
				return 0, nil, nil, err
			}
			if restored {
				result.RestoredGameIDs = append(result.RestoredGameIDs, item.GameID)
			} else {
				result.SkippedGameIDs = append(result.SkippedGameIDs, item.GameID)
			}
		}
		result.SkippedFolderIDs = sortedSetValues(skippedFolders)
		return 200, map[string]string{"Content-Type": "application/json; charset=utf-8"}, result, nil
	})
}

func restoreItem(
	ctx context.Context,
	games gameRecords, memberships membershipRecords,
	profileID string,
	item favorites.RestoreItem,
	existingFolders map[string]struct{},
	skippedFolders map[string]struct{},
	now int64,
) (bool, error) {
	visible, err := games.Visible(ctx, item.GameID)
	if err != nil || !visible {
		return false, err
	}
	if err := games.Ensure(ctx, profileID, item.GameID, now); err != nil {
		return false, fmt.Errorf("favorites/restoreItem: %w", err)
	}
	for _, folderID := range item.FolderIDs {
		if _, exists := existingFolders[folderID]; !exists {
			skippedFolders[folderID] = struct{}{}
			continue
		}
		if err := memberships.Add(ctx, profileID, folderID, item.GameID, now); err != nil {
			return false, fmt.Errorf("favorites/restoreItem: %w", err)
		}
	}
	return true, nil
}

func (repository *Repository) CommitCreateFolder(
	ctx context.Context, cmd favorites.CreateFolderCommand,
) (favorites.IdempotentResponse, error) {
	return repository.commitIdempotent(ctx, cmd.Idempotency, func(db dbexec.Executor) (
		int, map[string]string, any, error,
	) {
		games := gameRecords{db}
		folders := folderRecords{db}
		memberships := membershipRecords{db}

		count, err := folders.Count(ctx, cmd.ProfileID)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("favorites/CreateFolder: %w", err)
		}
		if count >= favorites.MaxFolders {
			return 0, nil, nil, favorites.ErrFolderLimit
		}
		if err := folders.RequireAvailableName(ctx, cmd.ProfileID, cmd.NameKey, ""); err != nil {
			return 0, nil, nil, fmt.Errorf("favorites/CreateFolder: %w", err)
		}
		if err := games.RequireVisible(ctx, cmd.GameIDs); err != nil {
			return 0, nil, nil, fmt.Errorf("favorites/CreateFolder: %w", err)
		}
		now := cmd.Idempotency.NowMS
		if err := folders.Create(ctx, favorites.FolderWrite{
			ProfileID: cmd.ProfileID, FolderID: cmd.FolderID,
			Name: cmd.Name, NameKey: cmd.NameKey, NowMS: now,
		}); err != nil {
			return 0, nil, nil, fmt.Errorf("favorites/CreateFolder: %w", err)
		}
		for _, gameID := range cmd.GameIDs {
			if err := games.Ensure(ctx, cmd.ProfileID, gameID, now); err != nil {
				return 0, nil, nil, fmt.Errorf("favorites/CreateFolder: %w", err)
			}
			if err := memberships.Add(ctx, cmd.ProfileID, cmd.FolderID, gameID, now); err != nil {
				return 0, nil, nil, fmt.Errorf("favorites/CreateFolder: %w", err)
			}
		}
		folder, err := folders.Get(ctx, cmd.ProfileID, cmd.FolderID)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("favorites/CreateFolder: %w", err)
		}
		headers := map[string]string{
			"Content-Type": "application/json; charset=utf-8",
			"Location":     "/api/v1/favorite-folders/" + cmd.FolderID,
			"ETag":         `"v1"`,
		}
		return 201, headers, folder, nil
	})
}

func (repository *Repository) CommitRenameFolder(
	ctx context.Context, cmd favorites.RenameFolderCommand,
) (favorites.IdempotentResponse, error) {
	return repository.commitIdempotent(ctx, cmd.Idempotency, func(db dbexec.Executor) (
		int, map[string]string, any, error,
	) {
		folders := folderRecords{db}

		folder, err := folders.Get(ctx, cmd.ProfileID, cmd.FolderID)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("favorites/RenameFolder: %w", err)
		}
		if folder.Version != cmd.ExpectedVersion {
			return 0, nil, nil, favorites.ErrVersionConflict
		}
		if folder.Name == cmd.Name {
			return 0, nil, nil, favorites.ErrInvalid
		}
		if err := folders.RequireAvailableName(ctx, cmd.ProfileID, cmd.NameKey, cmd.FolderID); err != nil {
			return 0, nil, nil, fmt.Errorf("favorites/RenameFolder: %w", err)
		}
		now := cmd.Idempotency.NowMS
		if err := folders.Rename(ctx, favorites.FolderWrite{
			ProfileID: cmd.ProfileID, FolderID: cmd.FolderID,
			Name: cmd.Name, NameKey: cmd.NameKey,
			ExpectedVersion: cmd.ExpectedVersion, NowMS: now,
		}); err != nil {
			return 0, nil, nil, fmt.Errorf("favorites: rename folder: %w", err)
		}
		folder, err = folders.Get(ctx, cmd.ProfileID, cmd.FolderID)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("favorites/RenameFolder: %w", err)
		}
		return 200, map[string]string{
			"Content-Type": "application/json; charset=utf-8",
			"ETag":         fmt.Sprintf(`"v%d"`, folder.Version),
		}, folder, nil
	})
}

func (repository *Repository) CommitDeleteFolder(
	ctx context.Context, cmd favorites.DeleteFolderCommand,
) (favorites.IdempotentResponse, error) {
	return repository.commitIdempotent(ctx, cmd.Idempotency, func(db dbexec.Executor) (
		int, map[string]string, any, error,
	) {
		folders := folderRecords{db}

		folder, err := folders.Get(ctx, cmd.ProfileID, cmd.FolderID)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("favorites/DeleteFolder: %w", err)
		}
		if folder.Version != cmd.ExpectedVersion {
			return 0, nil, nil, favorites.ErrVersionConflict
		}
		if err := folders.Delete(ctx, cmd.ProfileID, cmd.FolderID, cmd.ExpectedVersion); err != nil {
			return 0, nil, nil, fmt.Errorf("favorites/DeleteFolder: %w", err)
		}
		return 204, map[string]string{}, nil, nil
	})
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
WHERE profile_id=? AND game_id IN (SELECT value FROM json_each(?))`
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
AND membership.game_id IN (SELECT value FROM json_each(?))
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

func restoreRequestedFolderIDs(items []favorites.RestoreItem) []string {
	seen := make(map[string]struct{})
	for _, item := range items {
		for _, folderID := range item.FolderIDs {
			seen[folderID] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for folderID := range seen {
		result = append(result, folderID)
	}
	return result
}

func sortedSetValues(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	return result
}
