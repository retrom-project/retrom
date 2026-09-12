package favorites

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repository Repository
	now        func() time.Time
}

func New(repository Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func (service *Service) Favorite(ctx context.Context, principal Principal, gameID string) (State, error) {
	if !ValidID(gameID) {
		return State{}, ErrInvalid
	}
	var state State
	err := service.repository.WithWrite(ctx, func(connection WriteScope) error {
		visible, err := connection.Games.Visible(ctx, gameID)
		if err != nil {
			return repositoryError("Favorite", err)
		}
		if !visible {
			return ErrGameNotFound
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
			return ErrInvariant
		}
		return nil
	})
	return state, repositoryError("Favorite", err)
}

func replaceMemberships(
	ctx context.Context,
	database WriteScope,
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
	principal Principal,
	gameID string,
	folderIDs []string,
) (State, error) {
	if !ValidID(gameID) || validateUniqueIDs(folderIDs, MaxFolders) != nil {
		return State{}, ErrInvalid
	}
	desired := append([]string{}, folderIDs...)
	sort.Strings(desired)
	var state State
	err := service.repository.WithWrite(ctx, func(connection WriteScope) error {
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

func requestDigest(operation string, principal Principal, request any) string {
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
	principal Principal,
	operation, key string,
	request any,
	work func(WriteScope) (int, map[string]string, any, error),
) (IdempotentResponse, error) {
	digest := requestDigest(operation, principal, request)
	var response IdempotentResponse
	err := service.repository.WithWrite(ctx, func(connection WriteScope) error {
		now := service.now().UnixMilli()
		identity := IdempotencyKey{PrincipalID: principal.UserID, Operation: operation, Key: key}
		stored, found, err := connection.Idempotency.Find(ctx, identity, now)
		if err != nil {
			return repositoryError("idempotent", err)
		}
		if found {
			if stored.Digest != digest {
				return ErrIdempotencyReused
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
		response = IdempotentResponse{Status: status, Headers: headers, Body: body}
		if err := connection.Idempotency.Save(ctx, identity, IdempotencyRecord{
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
		return nil, nil, nil, ErrInvalid
	}
	if err := validateUniqueIDs(gameIDs, MaxOrganizeGames); err != nil {
		return nil, nil, nil, repositoryError("normalizeAndValidateOrganize", err)
	}
	if err := validateUniqueIDs(addFolderIDs, MaxOrganizeFolders); err != nil {
		return nil, nil, nil, repositoryError("normalizeAndValidateOrganize", err)
	}
	if err := validateUniqueIDs(removeFolderIDs, MaxOrganizeFolders); err != nil {
		return nil, nil, nil, repositoryError("normalizeAndValidateOrganize", err)
	}
	if len(gameIDs)*(len(addFolderIDs)+len(removeFolderIDs)) > MaxOrganizeEdges {
		return nil, nil, nil, ErrBatchTooLarge
	}
	removeSet := make(map[string]struct{}, len(removeFolderIDs))
	for _, folderID := range removeFolderIDs {
		removeSet[folderID] = struct{}{}
	}
	for _, folderID := range addFolderIDs {
		if _, overlap := removeSet[folderID]; overlap {
			return nil, nil, nil, ErrInvalid
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
	connection WriteScope,
	profileID, gameID string,
	add, remove []string,
	now int64,
) (State, bool, error) {
	if len(add) > 0 {
		if err := connection.Games.Ensure(ctx, profileID, gameID, now); err != nil {
			return State{}, false, repositoryError("organizeGame", err)
		}
	}
	for _, folderID := range remove {
		if err := connection.Memberships.Remove(ctx, profileID, folderID, gameID); err != nil {
			return State{}, false, repositoryError("organizeGame", err)
		}
	}
	for _, folderID := range add {
		if err := connection.Memberships.Add(ctx, profileID, folderID, gameID, now); err != nil {
			return State{}, false, repositoryError("organizeGame", err)
		}
	}
	state, exists, err := connection.Games.State(ctx, profileID, gameID)
	return state, exists, repositoryError("organizeGame", err)
}

func (service *Service) organizeWork(
	ctx context.Context,
	connection WriteScope,
	principal Principal,
	games, add, remove []string,
) (BatchResult, error) {
	if err := connection.Games.RequireVisible(ctx, games); err != nil {
		return BatchResult{}, repositoryError("organizeWork", err)
	}
	folders := append(append([]string{}, add...), remove...)
	if err := connection.Folders.Require(ctx, principal.ProfileID, folders); err != nil {
		return BatchResult{}, repositoryError("organizeWork", err)
	}
	result := BatchResult{Items: make([]State, 0, len(games))}
	now := service.now().UnixMilli()
	for _, gameID := range games {
		state, exists, err := organizeGame(ctx, connection, principal.ProfileID, gameID, add, remove, now)
		if err != nil {
			return BatchResult{}, repositoryError("organizeWork", err)
		}
		if exists {
			result.Items = append(result.Items, state)
		}
	}
	return result, nil
}

func (service *Service) Organize(
	ctx context.Context,
	principal Principal,
	key string,
	gameIDs, addFolderIDs, removeFolderIDs []string,
) (IdempotentResponse, error) {
	games, add, remove, err := normalizeAndValidateOrganize(gameIDs, addFolderIDs, removeFolderIDs)
	if err != nil {
		return IdempotentResponse{}, repositoryError("Organize", err)
	}
	request := struct {
		GameIDs         []string `json:"gameIds"`
		AddFolderIDs    []string `json:"addFolderIds"`
		RemoveFolderIDs []string `json:"removeFolderIds"`
	}{games, add, remove}
	return service.idempotent(ctx, principal, "postFavoriteOrganize", key, request,
		func(connection WriteScope) (int, map[string]string, any, error) {
			result, err := service.organizeWork(ctx, connection, principal, games, add, remove)
			if err != nil {
				return 0, nil, nil, repositoryError("Organize", err)
			}
			return 200, map[string]string{"Content-Type": "application/json; charset=utf-8"}, result, nil
		})
}

func (service *Service) Unfavorite(
	ctx context.Context,
	principal Principal,
	key string,
	gameIDs []string,
) (IdempotentResponse, error) {
	if len(gameIDs) == 0 {
		return IdempotentResponse{}, ErrInvalid
	}
	if err := validateUniqueIDs(gameIDs, MaxUnfavoriteGames); err != nil {
		return IdempotentResponse{}, repositoryError("Unfavorite", err)
	}
	games := append([]string{}, gameIDs...)
	sort.Strings(games)
	return service.idempotent(ctx, principal, "postFavoriteUnfavorite", key,
		struct {
			GameIDs []string `json:"gameIds"`
		}{games},
		func(connection WriteScope) (int, map[string]string, any, error) {
			result := UnfavoriteResult{Items: make([]UnfavoriteItem, 0, len(games))}
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
				result.Items = append(result.Items, UnfavoriteItem{GameID: gameID, FolderIDs: folderIDs})
				if err := connection.Games.Remove(ctx, principal.ProfileID, gameID); err != nil {
					return 0, nil, nil, repositoryError("Unfavorite", err)
				}
			}
			return 200, map[string]string{"Content-Type": "application/json; charset=utf-8"}, result, nil
		})
}

func requestedRestoreFolderIDs(items []RestoreItem) []string {
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
	connection WriteScope,
	profileID string,
	item RestoreItem,
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
	connection WriteScope,
	principal Principal,
	items []RestoreItem,
) (RestoreResult, error) {
	existingFolders, err := connection.Folders.Existing(ctx, principal.ProfileID,
		requestedRestoreFolderIDs(items),
	)
	if err != nil {
		return RestoreResult{}, repositoryError("restoreWork", err)
	}
	result := RestoreResult{RestoredGameIDs: []string{}, SkippedGameIDs: []string{}, SkippedFolderIDs: []string{}}
	skippedFolders := make(map[string]struct{})
	now := service.now().UnixMilli()
	for _, item := range items {
		restored, err := restoreItem(
			ctx, connection, principal.ProfileID, item, existingFolders, skippedFolders, now,
		)
		if err != nil {
			return RestoreResult{}, repositoryError("restoreWork", err)
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
	principal Principal,
	key string,
	items []RestoreItem,
) (IdempotentResponse, error) {
	canonical, err := normalizeRestoreItems(items)
	if err != nil {
		return IdempotentResponse{}, repositoryError("Restore", err)
	}
	return service.idempotent(ctx, principal, "postFavoriteRestore", key,
		struct {
			Items []RestoreItem `json:"items"`
		}{canonical},
		func(connection WriteScope) (int, map[string]string, any, error) {
			result, err := service.restoreWork(ctx, connection, principal, canonical)
			if err != nil {
				return 0, nil, nil, repositoryError("Restore", err)
			}
			return 200, map[string]string{"Content-Type": "application/json; charset=utf-8"}, result, nil
		})
}

func normalizeRestoreItems(items []RestoreItem) ([]RestoreItem, error) {
	if len(items) == 0 || len(items) > MaxRestoreGames {
		if len(items) > MaxRestoreGames {
			return nil, ErrBatchTooLarge
		}
		return nil, ErrInvalid
	}
	gameSeen := make(map[string]struct{}, len(items))
	edges := 0
	canonical := make([]RestoreItem, len(items))
	for index, item := range items {
		if !ValidID(item.GameID) {
			return nil, ErrInvalid
		}
		if _, duplicate := gameSeen[item.GameID]; duplicate {
			return nil, ErrInvalid
		}
		gameSeen[item.GameID] = struct{}{}
		if err := validateUniqueIDs(item.FolderIDs, MaxFolders); err != nil {
			return nil, repositoryError("normalizeRestoreItems", err)
		}
		edges += len(item.FolderIDs)
		canonical[index] = RestoreItem{GameID: item.GameID, FolderIDs: append([]string{}, item.FolderIDs...)}
		sort.Strings(canonical[index].FolderIDs)
	}
	if edges > MaxRestoreFolderEdges {
		return nil, ErrBatchTooLarge
	}
	sort.Slice(canonical, func(left, right int) bool { return canonical[left].GameID < canonical[right].GameID })
	return canonical, nil
}

func (service *Service) CreateFolder(
	ctx context.Context,
	principal Principal,
	key, rawName string,
	initialGameIDs []string,
) (IdempotentResponse, error) {
	name, nameKey, err := NormalizeFolderName(rawName)
	if err != nil {
		return IdempotentResponse{}, ErrInvalid
	}
	if err := validateUniqueIDs(initialGameIDs, MaxUnfavoriteGames); err != nil {
		return IdempotentResponse{}, repositoryError("CreateFolder", err)
	}
	games := append([]string{}, initialGameIDs...)
	sort.Strings(games)
	request := struct {
		Name           string   `json:"name"`
		InitialGameIDs []string `json:"initialGameIds"`
	}{name, games}
	return service.idempotent(ctx, principal, "postFavoriteFolder", key, request,
		func(connection WriteScope) (int, map[string]string, any, error) {
			count, err := connection.Folders.Count(ctx, principal.ProfileID)
			if err != nil {
				return 0, nil, nil, repositoryError("CreateFolder", err)
			}
			if count >= MaxFolders {
				return 0, nil, nil, ErrFolderLimit
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
			if err := connection.FolderWrites.Create(ctx, FolderWrite{
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
	principal Principal,
	key, folderID, rawName string,
	expectedVersion int64,
) (IdempotentResponse, error) {
	if !ValidID(folderID) || expectedVersion < 1 {
		return IdempotentResponse{}, ErrInvalid
	}
	name, nameKey, err := NormalizeFolderName(rawName)
	if err != nil {
		return IdempotentResponse{}, ErrInvalid
	}
	request := struct {
		FolderID string `json:"folderId"`
		Name     string `json:"name"`
		Version  int64  `json:"version"`
	}{folderID, name, expectedVersion}
	return service.idempotent(ctx, principal, "patchFavoriteFolder", key, request,
		func(connection WriteScope) (int, map[string]string, any, error) {
			folder, err := connection.Folders.Get(ctx, principal.ProfileID, folderID)
			if err != nil {
				return 0, nil, nil, repositoryError("RenameFolder", err)
			}
			if folder.Version != expectedVersion {
				return 0, nil, nil, ErrVersionConflict
			}
			if folder.Name == name {
				return 0, nil, nil, ErrInvalid
			}
			if err := connection.Folders.RequireAvailableName(ctx, principal.ProfileID, nameKey, folderID); err != nil {
				return 0, nil, nil, repositoryError("RenameFolder", err)
			}
			now := service.now().UnixMilli()
			err = connection.FolderWrites.Rename(ctx, FolderWrite{
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
	principal Principal,
	key, folderID string,
	expectedVersion int64,
) (IdempotentResponse, error) {
	if !ValidID(folderID) || expectedVersion < 1 {
		return IdempotentResponse{}, ErrInvalid
	}
	request := struct {
		FolderID string `json:"folderId"`
		Version  int64  `json:"version"`
	}{folderID, expectedVersion}
	return service.idempotent(ctx, principal, "deleteFavoriteFolder", key, request,
		func(connection WriteScope) (int, map[string]string, any, error) {
			folder, err := connection.Folders.Get(ctx, principal.ProfileID, folderID)
			if err != nil {
				return 0, nil, nil, repositoryError("DeleteFolder", err)
			}
			if folder.Version != expectedVersion {
				return 0, nil, nil, ErrVersionConflict
			}
			if err := connection.FolderWrites.Delete(ctx, principal.ProfileID, folderID, expectedVersion); err != nil {
				return 0, nil, nil, repositoryError("DeleteFolder", err)
			}
			return 204, map[string]string{}, nil, nil
		})
}

func (service *Service) Reference(ctx context.Context, profileID, gameID string) (*FavoriteReference, error) {
	reference, err := service.repository.Reference(ctx, profileID, gameID)
	return reference, repositoryError("Reference", err)
}

func (service *Service) References(
	ctx context.Context, profileID string, gameIDs []string,
) (map[string]FavoriteReference, error) {
	references, err := service.repository.References(ctx, profileID, gameIDs)
	return references, repositoryError("References", err)
}

func repositoryError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("favorites/%s: %w", operation, err)
}
