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
	state, err := service.repository.CommitFavorite(ctx, model.FavoriteCommand{
		ProfileID: principal.ProfileID,
		GameID:    gameID,
		NowMS:     service.now().UnixMilli(),
	})
	return state, repositoryError("Favorite", err)
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
	state, err := service.repository.CommitReplaceFolders(ctx, model.ReplaceFoldersCommand{
		ProfileID: principal.ProfileID,
		GameID:    gameID,
		FolderIDs: desired,
		NowMS:     service.now().UnixMilli(),
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

func (service *Service) idempotencyEnvelope(
	principal model.Principal, operation, key string, request any,
) model.IdempotencyEnvelope {
	now := service.now().UnixMilli()
	return model.IdempotencyEnvelope{
		PrincipalID: principal.UserID,
		Operation:   operation,
		Key:         key,
		Digest:      requestDigest(operation, principal, request),
		NowMS:       now,
		ExpiresAtMS: now + int64(24*time.Hour/time.Millisecond),
	}
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
	response, err := service.repository.CommitOrganize(ctx, model.OrganizeCommand{
		Idempotency:     service.idempotencyEnvelope(principal, "postFavoriteOrganize", key, request),
		ProfileID:       principal.ProfileID,
		GameIDs:         games,
		AddFolderIDs:    add,
		RemoveFolderIDs: remove,
	})
	return response, repositoryError("Organize", err)
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
	request := struct {
		GameIDs []string `json:"gameIds"`
	}{games}
	response, err := service.repository.CommitUnfavorite(ctx, model.UnfavoriteCommand{
		Idempotency: service.idempotencyEnvelope(principal, "postFavoriteUnfavorite", key, request),
		ProfileID:   principal.ProfileID,
		GameIDs:     games,
	})
	return response, repositoryError("Unfavorite", err)
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
	request := struct {
		Items []model.RestoreItem `json:"items"`
	}{canonical}
	response, err := service.repository.CommitRestore(ctx, model.RestoreCommand{
		Idempotency: service.idempotencyEnvelope(principal, "postFavoriteRestore", key, request),
		ProfileID:   principal.ProfileID,
		Items:       canonical,
	})
	return response, repositoryError("Restore", err)
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
	folderID, err := uuid.NewV7()
	if err != nil {
		return model.IdempotentResponse{}, fmt.Errorf("favorites: new folder id: %w", err)
	}
	request := struct {
		Name           string   `json:"name"`
		InitialGameIDs []string `json:"initialGameIds"`
	}{name, games}
	response, err := service.repository.CommitCreateFolder(ctx, model.CreateFolderCommand{
		Idempotency: service.idempotencyEnvelope(principal, "postFavoriteFolder", key, request),
		ProfileID:   principal.ProfileID,
		FolderID:    folderID.String(),
		Name:        name,
		NameKey:     nameKey,
		GameIDs:     games,
	})
	return response, repositoryError("CreateFolder", err)
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
	response, err := service.repository.CommitRenameFolder(ctx, model.RenameFolderCommand{
		Idempotency:     service.idempotencyEnvelope(principal, "patchFavoriteFolder", key, request),
		ProfileID:       principal.ProfileID,
		FolderID:        folderID,
		Name:            name,
		NameKey:         nameKey,
		ExpectedVersion: expectedVersion,
	})
	return response, repositoryError("RenameFolder", err)
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
	response, err := service.repository.CommitDeleteFolder(ctx, model.DeleteFolderCommand{
		Idempotency:     service.idempotencyEnvelope(principal, "deleteFavoriteFolder", key, request),
		ProfileID:       principal.ProfileID,
		FolderID:        folderID,
		ExpectedVersion: expectedVersion,
	})
	return response, repositoryError("DeleteFolder", err)
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
