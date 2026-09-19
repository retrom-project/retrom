package favorites

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	model "retrom/internal/model/favorites"

	"github.com/google/uuid"
)

type Service struct {
	repository model.Repository
	now        func() time.Time
}

func New(repository model.Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func (service *Service) Favorite(ctx context.Context, principal model.Principal, gameID string) (model.State, error) {
	if !model.ValidID(gameID) {
		return model.State{}, model.ErrInvalid
	}
	var state model.State
	err := service.repository.WithWrite(ctx, func(connection model.WriteScope) error {
		visible, err := connection.Games.Visible(ctx, gameID)
		if err != nil {
			return repositoryError("Favorite", err)
		}
		if !visible {
			return model.ErrGameNotFound
		}
		if err := connection.Games.Ensure(ctx, principal.ProfileID, gameID, service.now().UnixMilli()); err != nil {
			return fmt.Errorf("favorites: insert favorite: %w", err)
		}
		var exists bool
		state, exists, err = connection.Games.State(ctx, principal.ProfileID, gameID)
		if err != nil {
			return repositoryError("Favorite", err)
		}
		if !exists {
			return model.ErrInvariant
		}
		return nil
	})
	return state, repositoryError("Favorite", err)
}

func replaceMemberships(
	ctx context.Context,
	database model.WriteScope,
	profileID, gameID string,
	desired, current []string,
	now int64,
) error {
	desiredSet := make(map[string]struct{}, len(desired))
	for _, folderID := range desired {
		desiredSet[folderID] = struct{}{}
	}
	currentSet := make(map[string]struct{}, len(current))
	for _, folderID := range current {
		currentSet[folderID] = struct{}{}
		if _, keep := desiredSet[folderID]; !keep {
			if err := database.Memberships.Remove(ctx, profileID, folderID, gameID); err != nil {
				return repositoryError("replaceMemberships", err)
			}
		}
	}
	for _, folderID := range desired {
		if _, exists := currentSet[folderID]; !exists {
			if err := database.Memberships.Add(ctx, profileID, folderID, gameID, now); err != nil {
				return repositoryError("replaceMemberships", err)
			}
		}
	}
	return nil
}

func (service *Service) ReplaceFolders(
	ctx context.Context,
	principal model.Principal,
	gameID string,
	folderIDs []string,
) (model.State, error) {
	if !model.ValidID(gameID) || validateUniqueIDs(folderIDs, model.MaxFolders) != nil {
		return model.State{}, model.ErrInvalid
	}
	desired := append([]string{}, folderIDs...)
	sort.Strings(desired)
	var state model.State
	err := service.repository.WithWrite(ctx, func(connection model.WriteScope) error {
		if err := connection.Games.RequireVisible(ctx, []string{gameID}); err != nil {
			return repositoryError("ReplaceFolders", err)
		}
		if err := connection.Folders.Require(ctx, principal.ProfileID, desired); err != nil {
			return repositoryError("ReplaceFolders", err)
		}
		now := service.now().UnixMilli()
		if err := connection.Games.Ensure(ctx, principal.ProfileID, gameID, now); err != nil {
			return repositoryError("ReplaceFolders", err)
		}
		current, err := connection.Memberships.FolderIDs(ctx, principal.ProfileID, gameID)
		if err != nil {
			return repositoryError("ReplaceFolders", err)
		}
		if err := replaceMemberships(ctx, connection, principal.ProfileID, gameID, desired, current, now); err != nil {
			return repositoryError("ReplaceFolders", err)
		}
		state, _, err = connection.Games.State(ctx, principal.ProfileID, gameID)
		return repositoryError("ReplaceFolders", err)
	})
	return state, repositoryError("ReplaceFolders", err)
}

func requestDigest(operation string, principal model.Principal, request any) string {
	encoded, _ := json.Marshal(map[string]any{
		"operationId": operation,
		"principalId": principal.UserID,
		"request":     request,
	})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (service *Service) idempotent(
	ctx context.Context,
	principal model.Principal,
	operation, key string,
	request any,
	work func(model.WriteScope) (int, map[string]string, any, error),
) (model.IdempotentResponse, error) {
	digest := requestDigest(operation, principal, request)
	var response model.IdempotentResponse
	err := service.repository.WithWrite(ctx, func(connection model.WriteScope) error {
		now := service.now().UnixMilli()
		identity := model.IdempotencyKey{PrincipalID: principal.UserID, Operation: operation, Key: key}
		stored, found, err := connection.Idempotency.Find(ctx, identity, now)
		if err != nil {
			return repositoryError("idempotent", err)
		}
		if found {
			if stored.Digest != digest {
				return model.ErrIdempotencyReused
			}
			response = stored.Response
			response.Replayed = true
			return nil
		}
		status, headers, bodyValue, err := work(connection)
		if err != nil {
			return repositoryError("idempotent", err)
		}
		body := []byte{}
		if bodyValue != nil {
			body, err = json.Marshal(bodyValue)
			if err != nil {
				return fmt.Errorf("favorites: encode idempotent response: %w", err)
			}
			body = append(body, '\n')
		}
		response = model.IdempotentResponse{Status: status, Headers: headers, Body: body}
		if err := connection.Idempotency.Save(ctx, identity, model.IdempotencyRecord{
			Digest: digest, Response: response, CreatedAtMS: now,
			ExpiresAtMS: now + int64(24*time.Hour/time.Millisecond),
		}); err != nil {
			return repositoryError("idempotent", err)
		}
		return nil
	})
	return response, repositoryError("idempotent", err)
}

func normalizeAndValidateOrganize(
	gameIDs, addFolderIDs, removeFolderIDs []string,
) ([]string, []string, []string, error) {
	if len(gameIDs) == 0 || (len(addFolderIDs) == 0 && len(removeFolderIDs) == 0) {
		return nil, nil, nil, model.ErrInvalid
	}
	if err := validateUniqueIDs(gameIDs, model.MaxOrganizeGames); err != nil {
		return nil, nil, nil, repositoryError("normalizeAndValidateOrganize", err)
	}
	if err := validateUniqueIDs(addFolderIDs, model.MaxOrganizeFolders); err != nil {
		return nil, nil, nil, repositoryError("normalizeAndValidateOrganize", err)
	}
	if err := validateUniqueIDs(removeFolderIDs, model.MaxOrganizeFolders); err != nil {
		return nil, nil, nil, repositoryError("normalizeAndValidateOrganize", err)
	}
	if len(gameIDs)*(len(addFolderIDs)+len(removeFolderIDs)) > model.MaxOrganizeEdges {
		return nil, nil, nil, model.ErrBatchTooLarge
	}
	removeSet := make(map[string]struct{}, len(removeFolderIDs))
	for _, folderID := range removeFolderIDs {
		removeSet[folderID] = struct{}{}
	}
	for _, folderID := range addFolderIDs {
		if _, overlap := removeSet[folderID]; overlap {
			return nil, nil, nil, model.ErrInvalid
		}
	}
	games := append([]string{}, gameIDs...)
	add := append([]string{}, addFolderIDs...)
	remove := append([]string{}, removeFolderIDs...)
	sort.Strings(games)
	sort.Strings(add)
	sort.Strings(remove)
	return games, add, remove, nil
}

func organizeGame(
	ctx context.Context,
	connection model.WriteScope,
	profileID, gameID string,
	add, remove []string,
	now int64,
) (model.State, bool, error) {
	if len(add) > 0 {
		if err := connection.Games.Ensure(ctx, profileID, gameID, now); err != nil {
			return model.State{}, false, repositoryError("organizeGame", err)
		}
	}
	for _, folderID := range remove {
		if err := connection.Memberships.Remove(ctx, profileID, folderID, gameID); err != nil {
			return model.State{}, false, repositoryError("organizeGame", err)
		}
	}
	for _, folderID := range add {
		if err := connection.Memberships.Add(ctx, profileID, folderID, gameID, now); err != nil {
			return model.State{}, false, repositoryError("organizeGame", err)
		}
	}
	state, exists, err := connection.Games.State(ctx, profileID, gameID)
	return state, exists, repositoryError("organizeGame", err)
}

func (service *Service) organizeWork(
	ctx context.Context,
	connection model.WriteScope,
	principal model.Principal,
	games, add, remove []string,
) (model.BatchResult, error) {
	if err := connection.Games.RequireVisible(ctx, games); err != nil {
		return model.BatchResult{}, repositoryError("organizeWork", err)
	}
	folders := append(append([]string{}, add...), remove...)
	if err := connection.Folders.Require(ctx, principal.ProfileID, folders); err != nil {
		return model.BatchResult{}, repositoryError("organizeWork", err)
	}
	result := model.BatchResult{Items: make([]model.State, 0, len(games))}
	now := service.now().UnixMilli()
	for _, gameID := range games {
		state, exists, err := organizeGame(ctx, connection, principal.ProfileID, gameID, add, remove, now)
		if err != nil {
			return model.BatchResult{}, repositoryError("organizeWork", err)
		}
		if exists {
			result.Items = append(result.Items, state)
		}
	}
	return result, nil
}

func (service *Service) Organize(
	ctx context.Context,
	principal model.Principal,
	key string,
	gameIDs, addFolderIDs, removeFolderIDs []string,
) (model.IdempotentResponse, error) {
	games, add, remove, err := normalizeAndValidateOrganize(gameIDs, addFolderIDs, removeFolderIDs)
	if err != nil {
		return model.IdempotentResponse{}, repositoryError("Organize", err)
	}
	request := struct {
		GameIDs         []string `json:"gameIds"`
		AddFolderIDs    []string `json:"addFolderIds"`
		RemoveFolderIDs []string `json:"removeFolderIds"`
	}{games, add, remove}
	return service.idempotent(ctx, principal, "postFavoriteOrganize", key, request,
		func(connection model.WriteScope) (int, map[string]string, any, error) {
			result, err := service.organizeWork(ctx, connection, principal, games, add, remove)
			if err != nil {
				return 0, nil, nil, repositoryError("Organize", err)
			}
			return 200, map[string]string{"Content-Type": "application/json; charset=utf-8"}, result, nil
		})
}

func (service *Service) Unfavorite(
	ctx context.Context,
	principal model.Principal,
	key string,
	gameIDs []string,
) (model.IdempotentResponse, error) {
	if len(gameIDs) == 0 {
		return model.IdempotentResponse{}, model.ErrInvalid
	}
	if err := validateUniqueIDs(gameIDs, model.MaxUnfavoriteGames); err != nil {
		return model.IdempotentResponse{}, repositoryError("Unfavorite", err)
	}
	games := append([]string{}, gameIDs...)
	sort.Strings(games)
	return service.idempotent(ctx, principal, "postFavoriteUnfavorite", key,
		struct {
			GameIDs []string `json:"gameIds"`
		}{games},
		func(connection model.WriteScope) (int, map[string]string, any, error) {
			result := model.UnfavoriteResult{Items: make([]model.UnfavoriteItem, 0, len(games))}
			for _, gameID := range games {
				_, exists, err := connection.Games.State(ctx, principal.ProfileID, gameID)
				if err != nil {
					return 0, nil, nil, repositoryError("Unfavorite", err)
				}
				if !exists {
					continue
				}
				folderIDs, err := connection.Memberships.FolderIDs(ctx, principal.ProfileID, gameID)
				if err != nil {
					return 0, nil, nil, repositoryError("Unfavorite", err)
				}
				result.Items = append(result.Items, model.UnfavoriteItem{GameID: gameID, FolderIDs: folderIDs})
				if err := connection.Games.Remove(ctx, principal.ProfileID, gameID); err != nil {
					return 0, nil, nil, repositoryError("Unfavorite", err)
				}
			}
			return 200, map[string]string{"Content-Type": "application/json; charset=utf-8"}, result, nil
		})
}

func requestedRestoreFolderIDs(items []model.RestoreItem) []string {
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
	sort.Strings(result)
	return result
}

func restoreItem(
	ctx context.Context,
	connection model.WriteScope,
	profileID string,
	item model.RestoreItem,
	existingFolders map[string]struct{},
	skippedFolders map[string]struct{},
	now int64,
) (bool, error) {
	visible, err := connection.Games.Visible(ctx, item.GameID)
	if err != nil || !visible {
		return false, repositoryError("restoreItem", err)
	}
	if err := connection.Games.Ensure(ctx, profileID, item.GameID, now); err != nil {
		return false, repositoryError("restoreItem", err)
	}
	for _, folderID := range item.FolderIDs {
		if _, exists := existingFolders[folderID]; !exists {
			skippedFolders[folderID] = struct{}{}
			continue
		}
		if err := connection.Memberships.Add(ctx, profileID, folderID, item.GameID, now); err != nil {
			return false, repositoryError("restoreItem", err)
		}
	}
	return true, nil
}

func sortedSetValues(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (service *Service) restoreWork(
	ctx context.Context,
	connection model.WriteScope,
	principal model.Principal,
	items []model.RestoreItem,
) (model.RestoreResult, error) {
	existingFolders, err := connection.Folders.Existing(ctx, principal.ProfileID,
		requestedRestoreFolderIDs(items),
	)
	if err != nil {
		return model.RestoreResult{}, repositoryError("restoreWork", err)
	}
	result := model.RestoreResult{RestoredGameIDs: []string{}, SkippedGameIDs: []string{}, SkippedFolderIDs: []string{}}
	skippedFolders := make(map[string]struct{})
	now := service.now().UnixMilli()
	for _, item := range items {
		restored, err := restoreItem(
			ctx, connection, principal.ProfileID, item, existingFolders, skippedFolders, now,
		)
		if err != nil {
			return model.RestoreResult{}, repositoryError("restoreWork", err)
		}
		if restored {
			result.RestoredGameIDs = append(result.RestoredGameIDs, item.GameID)
		} else {
			result.SkippedGameIDs = append(result.SkippedGameIDs, item.GameID)
		}
	}
	result.SkippedFolderIDs = sortedSetValues(skippedFolders)
	return result, nil
}

func (service *Service) Restore(
	ctx context.Context,
	principal model.Principal,
	key string,
	items []model.RestoreItem,
) (model.IdempotentResponse, error) {
	canonical, err := normalizeRestoreItems(items)
	if err != nil {
		return model.IdempotentResponse{}, repositoryError("Restore", err)
	}
	return service.idempotent(ctx, principal, "postFavoriteRestore", key,
		struct {
			Items []model.RestoreItem `json:"items"`
		}{canonical},
		func(connection model.WriteScope) (int, map[string]string, any, error) {
			result, err := service.restoreWork(ctx, connection, principal, canonical)
			if err != nil {
				return 0, nil, nil, repositoryError("Restore", err)
			}
			return 200, map[string]string{"Content-Type": "application/json; charset=utf-8"}, result, nil
		})
}

func normalizeRestoreItems(items []model.RestoreItem) ([]model.RestoreItem, error) {
	if len(items) == 0 || len(items) > model.MaxRestoreGames {
		if len(items) > model.MaxRestoreGames {
			return nil, model.ErrBatchTooLarge
		}
		return nil, model.ErrInvalid
	}
	gameSeen := make(map[string]struct{}, len(items))
	edges := 0
	canonical := make([]model.RestoreItem, len(items))
	for index, item := range items {
		if !model.ValidID(item.GameID) {
			return nil, model.ErrInvalid
		}
		if _, duplicate := gameSeen[item.GameID]; duplicate {
			return nil, model.ErrInvalid
		}
		gameSeen[item.GameID] = struct{}{}
		if err := validateUniqueIDs(item.FolderIDs, model.MaxFolders); err != nil {
			return nil, repositoryError("normalizeRestoreItems", err)
		}
		edges += len(item.FolderIDs)
		canonical[index] = model.RestoreItem{GameID: item.GameID, FolderIDs: append([]string{}, item.FolderIDs...)}
		sort.Strings(canonical[index].FolderIDs)
	}
	if edges > model.MaxRestoreFolderEdges {
		return nil, model.ErrBatchTooLarge
	}
	sort.Slice(canonical, func(left, right int) bool { return canonical[left].GameID < canonical[right].GameID })
	return canonical, nil
}

func (service *Service) CreateFolder(
	ctx context.Context,
	principal model.Principal,
	key, rawName string,
	initialGameIDs []string,
) (model.IdempotentResponse, error) {
	name, nameKey, err := NormalizeFolderName(rawName)
	if err != nil {
		return model.IdempotentResponse{}, model.ErrInvalid
	}
	if err := validateUniqueIDs(initialGameIDs, model.MaxUnfavoriteGames); err != nil {
		return model.IdempotentResponse{}, repositoryError("CreateFolder", err)
	}
	games := append([]string{}, initialGameIDs...)
	sort.Strings(games)
	request := struct {
		Name           string   `json:"name"`
		InitialGameIDs []string `json:"initialGameIds"`
	}{name, games}
	return service.idempotent(ctx, principal, "postFavoriteFolder", key, request,
		func(connection model.WriteScope) (int, map[string]string, any, error) {
			count, err := connection.Folders.Count(ctx, principal.ProfileID)
			if err != nil {
				return 0, nil, nil, repositoryError("CreateFolder", err)
			}
			if count >= model.MaxFolders {
				return 0, nil, nil, model.ErrFolderLimit
			}
			if err := connection.Folders.RequireAvailableName(ctx, principal.ProfileID, nameKey, ""); err != nil {
				return 0, nil, nil, repositoryError("CreateFolder", err)
			}
			if err := connection.Games.RequireVisible(ctx, games); err != nil {
				return 0, nil, nil, repositoryError("CreateFolder", err)
			}
			folderID, err := uuid.NewV7()
			if err != nil {
				return 0, nil, nil, fmt.Errorf("favorites: new folder id: %w", err)
			}
			now := service.now().UnixMilli()
			if err := connection.FolderWrites.Create(ctx, model.FolderWrite{
				ProfileID: principal.ProfileID, FolderID: folderID.String(),
				Name: name, NameKey: nameKey, NowMS: now,
			}); err != nil {
				return 0, nil, nil, repositoryError("CreateFolder", err)
			}
			for _, gameID := range games {
				if err := connection.Games.Ensure(ctx, principal.ProfileID, gameID, now); err != nil {
					return 0, nil, nil, repositoryError("CreateFolder", err)
				}
				if err := connection.Memberships.Add(ctx, principal.ProfileID, folderID.String(), gameID, now); err != nil {
					return 0, nil, nil, repositoryError("CreateFolder", err)
				}
			}
			folder, err := connection.Folders.Get(ctx, principal.ProfileID, folderID.String())
			if err != nil {
				return 0, nil, nil, repositoryError("CreateFolder", err)
			}
			headers := map[string]string{
				"Content-Type": "application/json; charset=utf-8",
				"Location":     "/api/v1/favorite-folders/" + folderID.String(),
				"ETag":         `"v1"`,
			}
			return 201, headers, folder, nil
		})
}

func (service *Service) RenameFolder(
	ctx context.Context,
	principal model.Principal,
	key, folderID, rawName string,
	expectedVersion int64,
) (model.IdempotentResponse, error) {
	if !model.ValidID(folderID) || expectedVersion < 1 {
		return model.IdempotentResponse{}, model.ErrInvalid
	}
	name, nameKey, err := NormalizeFolderName(rawName)
	if err != nil {
		return model.IdempotentResponse{}, model.ErrInvalid
	}
	request := struct {
		FolderID string `json:"folderId"`
		Name     string `json:"name"`
		Version  int64  `json:"version"`
	}{folderID, name, expectedVersion}
	return service.idempotent(ctx, principal, "patchFavoriteFolder", key, request,
		func(connection model.WriteScope) (int, map[string]string, any, error) {
			folder, err := connection.Folders.Get(ctx, principal.ProfileID, folderID)
			if err != nil {
				return 0, nil, nil, repositoryError("RenameFolder", err)
			}
			if folder.Version != expectedVersion {
				return 0, nil, nil, model.ErrVersionConflict
			}
			if folder.Name == name {
				return 0, nil, nil, model.ErrInvalid
			}
			if err := connection.Folders.RequireAvailableName(ctx, principal.ProfileID, nameKey, folderID); err != nil {
				return 0, nil, nil, repositoryError("RenameFolder", err)
			}
			now := service.now().UnixMilli()
			err = connection.FolderWrites.Rename(ctx, model.FolderWrite{
				ProfileID: principal.ProfileID, FolderID: folderID, Name: name, NameKey: nameKey,
				ExpectedVersion: expectedVersion, NowMS: now,
			})
			if err != nil {
				return 0, nil, nil, fmt.Errorf("favorites: rename folder: %w", err)
			}
			folder, err = connection.Folders.Get(ctx, principal.ProfileID, folderID)
			if err != nil {
				return 0, nil, nil, repositoryError("RenameFolder", err)
			}
			return 200, map[string]string{
				"Content-Type": "application/json; charset=utf-8", "ETag": fmt.Sprintf(`"v%d"`, folder.Version),
			}, folder, nil
		})
}

func (service *Service) DeleteFolder(
	ctx context.Context,
	principal model.Principal,
	key, folderID string,
	expectedVersion int64,
) (model.IdempotentResponse, error) {
	if !model.ValidID(folderID) || expectedVersion < 1 {
		return model.IdempotentResponse{}, model.ErrInvalid
	}
	request := struct {
		FolderID string `json:"folderId"`
		Version  int64  `json:"version"`
	}{folderID, expectedVersion}
	return service.idempotent(ctx, principal, "deleteFavoriteFolder", key, request,
		func(connection model.WriteScope) (int, map[string]string, any, error) {
			folder, err := connection.Folders.Get(ctx, principal.ProfileID, folderID)
			if err != nil {
				return 0, nil, nil, repositoryError("DeleteFolder", err)
			}
			if folder.Version != expectedVersion {
				return 0, nil, nil, model.ErrVersionConflict
			}
			if err := connection.FolderWrites.Delete(ctx, principal.ProfileID, folderID, expectedVersion); err != nil {
				return 0, nil, nil, repositoryError("DeleteFolder", err)
			}
			return 204, map[string]string{}, nil, nil
		})
}

func (service *Service) Reference(ctx context.Context, profileID, gameID string) (*model.FavoriteReference, error) {
	reference, err := service.repository.Reference(ctx, profileID, gameID)
	return reference, repositoryError("Reference", err)
}

func (service *Service) References(
	ctx context.Context, profileID string, gameIDs []string,
) (map[string]model.FavoriteReference, error) {
	references, err := service.repository.References(ctx, profileID, gameIDs)
	return references, repositoryError("References", err)
}

func repositoryError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("favorites/%s: %w", operation, err)
}
