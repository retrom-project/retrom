package favorites

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	database *sql.DB
	now      func() time.Time
}

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func New(database *sql.DB, now func() time.Time) *Service {
	return &Service{database: database, now: now}
}

func encodedStringList(values []string) string {
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

func (service *Service) withImmediateWrite(ctx context.Context, work func(*sql.Conn) error) error {
	connection, err := service.database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("favorites: acquire connection: %w", err)
	}
	defer func() { _ = connection.Close() }()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("favorites: begin immediate: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()
	if err := work(connection); err != nil {
		return err
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("favorites: commit: %w", err)
	}
	committed = true
	return nil
}

func visibleGame(ctx context.Context, database executor, gameID string) (bool, error) {
	var found int
	err := database.QueryRowContext(ctx, `
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

func validateVisibleGames(ctx context.Context, database executor, gameIDs []string) error {
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
	if err := database.QueryRowContext(ctx, query, encodedStringList(gameIDs)).Scan(&count); err != nil {
		return fmt.Errorf("favorites: validate games: %w", err)
	}
	if count != len(gameIDs) {
		return ErrGameNotFound
	}
	return nil
}

func validateFolders(ctx context.Context, database executor, profileID string, folderIDs []string) error {
	if len(folderIDs) == 0 {
		return nil
	}
	var count int
	query := `
SELECT count(*)
FROM favorite_folders
WHERE profile_id=? AND id IN (SELECT value FROM json_each(?))`
	if err := database.QueryRowContext(ctx, query, profileID, encodedStringList(folderIDs)).Scan(&count); err != nil {
		return fmt.Errorf("favorites: validate folders: %w", err)
	}
	if count != len(folderIDs) {
		return ErrFolderNotFound
	}
	return nil
}

func folderIDsForGame(ctx context.Context, database executor, profileID, gameID string) ([]string, error) {
	rows, err := database.QueryContext(ctx, `
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

func stateForGame(ctx context.Context, database executor, profileID, gameID string) (State, bool, error) {
	var createdAtMS int64
	err := database.QueryRowContext(ctx, `
SELECT created_at_ms FROM favorite_games WHERE profile_id=? AND game_id=?
`, profileID, gameID).Scan(&createdAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, fmt.Errorf("favorites: read favorite: %w", err)
	}
	folderIDs, err := folderIDsForGame(ctx, database, profileID, gameID)
	if err != nil {
		return State{}, false, err
	}
	return State{GameID: gameID, FavoritedAtMS: createdAtMS, FolderIDs: folderIDs}, true, nil
}

func (service *Service) Reference(ctx context.Context, profileID, gameID string) (*FavoriteReference, error) {
	state, exists, err := stateForGame(ctx, service.database, profileID, gameID)
	if err != nil || !exists {
		return nil, err
	}
	return &FavoriteReference{FavoritedAtMS: state.FavoritedAtMS, FolderIDs: state.FolderIDs}, nil
}

func (service *Service) References(
	ctx context.Context,
	profileID string,
	gameIDs []string,
) (map[string]FavoriteReference, error) {
	result := make(map[string]FavoriteReference)
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
		result[gameID] = FavoriteReference{FavoritedAtMS: createdAtMS, FolderIDs: []string{}}
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

func (service *Service) Favorite(ctx context.Context, principal Principal, gameID string) (State, error) {
	if !ValidID(gameID) {
		return State{}, ErrInvalid
	}
	var state State
	err := service.withImmediateWrite(ctx, func(connection *sql.Conn) error {
		visible, err := visibleGame(ctx, connection, gameID)
		if err != nil {
			return err
		}
		if !visible {
			return ErrGameNotFound
		}
		if _, err := connection.ExecContext(ctx, `
INSERT INTO favorite_games(profile_id,game_id,created_at_ms) VALUES(?,?,?)
ON CONFLICT(profile_id,game_id) DO NOTHING
`, principal.ProfileID, gameID, service.now().UnixMilli()); err != nil {
			return fmt.Errorf("favorites: insert favorite: %w", err)
		}
		var exists bool
		state, exists, err = stateForGame(ctx, connection, principal.ProfileID, gameID)
		if err != nil {
			return err
		}
		if !exists {
			return ErrInvariant
		}
		return nil
	})
	return state, err
}

func ensureFavorite(ctx context.Context, database executor, profileID, gameID string, now int64) error {
	if _, err := database.ExecContext(ctx, `
INSERT INTO favorite_games(profile_id,game_id,created_at_ms) VALUES(?,?,?)
ON CONFLICT(profile_id,game_id) DO NOTHING
`, profileID, gameID, now); err != nil {
		return fmt.Errorf("favorites: ensure favorite: %w", err)
	}
	return nil
}

func removeMembership(ctx context.Context, database executor, profileID, folderID, gameID string) error {
	if _, err := database.ExecContext(ctx, `
DELETE FROM favorite_folder_games WHERE profile_id=? AND folder_id=? AND game_id=?
`, profileID, folderID, gameID); err != nil {
		return fmt.Errorf("favorites: remove membership: %w", err)
	}
	return nil
}

func addMembership(
	ctx context.Context,
	database executor,
	profileID, folderID, gameID string,
	now int64,
) error {
	if _, err := database.ExecContext(ctx, `
INSERT INTO favorite_folder_games(profile_id,folder_id,game_id,created_at_ms) VALUES(?,?,?,?)
ON CONFLICT(profile_id,folder_id,game_id) DO NOTHING
`, profileID, folderID, gameID, now); err != nil {
		return fmt.Errorf("favorites: add membership: %w", err)
	}
	return nil
}

func replaceMemberships(
	ctx context.Context,
	database executor,
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
			if err := removeMembership(ctx, database, profileID, folderID, gameID); err != nil {
				return err
			}
		}
	}
	for _, folderID := range desired {
		if _, exists := currentSet[folderID]; !exists {
			if err := addMembership(ctx, database, profileID, folderID, gameID, now); err != nil {
				return err
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
	err := service.withImmediateWrite(ctx, func(connection *sql.Conn) error {
		if err := validateVisibleGames(ctx, connection, []string{gameID}); err != nil {
			return err
		}
		if err := validateFolders(ctx, connection, principal.ProfileID, desired); err != nil {
			return err
		}
		now := service.now().UnixMilli()
		if err := ensureFavorite(ctx, connection, principal.ProfileID, gameID, now); err != nil {
			return err
		}
		current, err := folderIDsForGame(ctx, connection, principal.ProfileID, gameID)
		if err != nil {
			return err
		}
		if err := replaceMemberships(ctx, connection, principal.ProfileID, gameID, desired, current, now); err != nil {
			return err
		}
		state, _, err = stateForGame(ctx, connection, principal.ProfileID, gameID)
		return err
	})
	return state, err
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
	work func(*sql.Conn) (int, map[string]string, any, error),
) (IdempotentResponse, error) {
	digest := requestDigest(operation, principal, request)
	var response IdempotentResponse
	err := service.withImmediateWrite(ctx, func(connection *sql.Conn) error {
		now := service.now().UnixMilli()
		if _, err := connection.ExecContext(ctx, `
DELETE FROM idempotency_records
WHERE principal_id=? AND operation_id=? AND key=? AND expires_at_ms<=?
`, principal.UserID, operation, key, now); err != nil {
			return fmt.Errorf("favorites: prune idempotency: %w", err)
		}
		var storedDigest, headersJSON string
		var storedStatus int
		var storedBody []byte
		err := connection.QueryRowContext(ctx, `
SELECT request_digest,http_status,response_headers_json,response_body
FROM idempotency_records
WHERE principal_id=? AND operation_id=? AND key=?
`, principal.UserID, operation, key).Scan(&storedDigest, &storedStatus, &headersJSON, &storedBody)
		if err == nil {
			if storedDigest != digest {
				return ErrIdempotencyReused
			}
			headers := make(map[string]string)
			if err := json.Unmarshal([]byte(headersJSON), &headers); err != nil {
				return fmt.Errorf("favorites: decode idempotency headers: %w", err)
			}
			response = IdempotentResponse{Status: storedStatus, Headers: headers, Body: storedBody, Replayed: true}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("favorites: read idempotency: %w", err)
		}
		status, headers, bodyValue, err := work(connection)
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
		headersJSONBytes, _ := json.Marshal(headers)
		if _, err := connection.ExecContext(ctx, `
INSERT INTO idempotency_records(
  principal_id,operation_id,key,request_digest,http_status,response_headers_json,response_body,
  created_at_ms,expires_at_ms
) VALUES(?,?,?,?,?,?,?,?,?)
`, principal.UserID, operation, key, digest, status, string(headersJSONBytes), body, now,
			now+int64(24*time.Hour/time.Millisecond)); err != nil {
			return fmt.Errorf("favorites: store idempotency: %w", err)
		}
		response = IdempotentResponse{Status: status, Headers: headers, Body: body}
		return nil
	})
	return response, err
}

func normalizeAndValidateOrganize(
	gameIDs, addFolderIDs, removeFolderIDs []string,
) ([]string, []string, []string, error) {
	if len(gameIDs) == 0 || (len(addFolderIDs) == 0 && len(removeFolderIDs) == 0) {
		return nil, nil, nil, ErrInvalid
	}
	if err := validateUniqueIDs(gameIDs, MaxOrganizeGames); err != nil {
		return nil, nil, nil, err
	}
	if err := validateUniqueIDs(addFolderIDs, MaxOrganizeFolders); err != nil {
		return nil, nil, nil, err
	}
	if err := validateUniqueIDs(removeFolderIDs, MaxOrganizeFolders); err != nil {
		return nil, nil, nil, err
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
	connection *sql.Conn,
	profileID, gameID string,
	add, remove []string,
	now int64,
) (State, bool, error) {
	if len(add) > 0 {
		if err := ensureFavorite(ctx, connection, profileID, gameID, now); err != nil {
			return State{}, false, err
		}
	}
	for _, folderID := range remove {
		if err := removeMembership(ctx, connection, profileID, folderID, gameID); err != nil {
			return State{}, false, err
		}
	}
	for _, folderID := range add {
		if err := addMembership(ctx, connection, profileID, folderID, gameID, now); err != nil {
			return State{}, false, err
		}
	}
	return stateForGame(ctx, connection, profileID, gameID)
}

func (service *Service) organizeWork(
	ctx context.Context,
	connection *sql.Conn,
	principal Principal,
	games, add, remove []string,
) (BatchResult, error) {
	if err := validateVisibleGames(ctx, connection, games); err != nil {
		return BatchResult{}, err
	}
	folders := append(append([]string{}, add...), remove...)
	if err := validateFolders(ctx, connection, principal.ProfileID, folders); err != nil {
		return BatchResult{}, err
	}
	result := BatchResult{Items: make([]State, 0, len(games))}
	now := service.now().UnixMilli()
	for _, gameID := range games {
		state, exists, err := organizeGame(ctx, connection, principal.ProfileID, gameID, add, remove, now)
		if err != nil {
			return BatchResult{}, err
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
		return IdempotentResponse{}, err
	}
	request := struct {
		GameIDs         []string `json:"gameIds"`
		AddFolderIDs    []string `json:"addFolderIds"`
		RemoveFolderIDs []string `json:"removeFolderIds"`
	}{games, add, remove}
	return service.idempotent(ctx, principal, "postFavoriteOrganize", key, request,
		func(connection *sql.Conn) (int, map[string]string, any, error) {
			result, err := service.organizeWork(ctx, connection, principal, games, add, remove)
			if err != nil {
				return 0, nil, nil, err
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
		return IdempotentResponse{}, err
	}
	games := append([]string{}, gameIDs...)
	sort.Strings(games)
	return service.idempotent(ctx, principal, "postFavoriteUnfavorite", key,
		struct {
			GameIDs []string `json:"gameIds"`
		}{games},
		func(connection *sql.Conn) (int, map[string]string, any, error) {
			result := UnfavoriteResult{Items: make([]UnfavoriteItem, 0, len(games))}
			for _, gameID := range games {
				_, exists, err := stateForGame(ctx, connection, principal.ProfileID, gameID)
				if err != nil {
					return 0, nil, nil, err
				}
				if !exists {
					continue
				}
				folderIDs, err := folderIDsForGame(ctx, connection, principal.ProfileID, gameID)
				if err != nil {
					return 0, nil, nil, err
				}
				result.Items = append(result.Items, UnfavoriteItem{GameID: gameID, FolderIDs: folderIDs})
				if _, err := connection.ExecContext(ctx, `
DELETE FROM favorite_folder_games WHERE profile_id=? AND game_id=?
`, principal.ProfileID, gameID); err != nil {
					return 0, nil, nil, fmt.Errorf("favorites: remove favorite memberships: %w", err)
				}
				if _, err := connection.ExecContext(ctx, `
DELETE FROM favorite_games WHERE profile_id=? AND game_id=?
`, principal.ProfileID, gameID); err != nil {
					return 0, nil, nil, fmt.Errorf("favorites: remove favorite: %w", err)
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

func existingRestoreFolders(
	ctx context.Context,
	connection *sql.Conn,
	profileID string,
	requested []string,
) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(requested))
	if len(requested) == 0 {
		return result, nil
	}
	rows, err := connection.QueryContext(ctx, `
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

func restoreItem(
	ctx context.Context,
	connection *sql.Conn,
	profileID string,
	item RestoreItem,
	existingFolders map[string]struct{},
	skippedFolders map[string]struct{},
	now int64,
) (bool, error) {
	visible, err := visibleGame(ctx, connection, item.GameID)
	if err != nil || !visible {
		return false, err
	}
	if err := ensureFavorite(ctx, connection, profileID, item.GameID, now); err != nil {
		return false, err
	}
	for _, folderID := range item.FolderIDs {
		if _, exists := existingFolders[folderID]; !exists {
			skippedFolders[folderID] = struct{}{}
			continue
		}
		if err := addMembership(ctx, connection, profileID, folderID, item.GameID, now); err != nil {
			return false, err
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
	connection *sql.Conn,
	principal Principal,
	items []RestoreItem,
) (RestoreResult, error) {
	existingFolders, err := existingRestoreFolders(
		ctx,
		connection,
		principal.ProfileID,
		requestedRestoreFolderIDs(items),
	)
	if err != nil {
		return RestoreResult{}, err
	}
	result := RestoreResult{RestoredGameIDs: []string{}, SkippedGameIDs: []string{}, SkippedFolderIDs: []string{}}
	skippedFolders := make(map[string]struct{})
	now := service.now().UnixMilli()
	for _, item := range items {
		restored, err := restoreItem(
			ctx, connection, principal.ProfileID, item, existingFolders, skippedFolders, now,
		)
		if err != nil {
			return RestoreResult{}, err
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
		return IdempotentResponse{}, err
	}
	return service.idempotent(ctx, principal, "postFavoriteRestore", key,
		struct {
			Items []RestoreItem `json:"items"`
		}{canonical},
		func(connection *sql.Conn) (int, map[string]string, any, error) {
			result, err := service.restoreWork(ctx, connection, principal, canonical)
			if err != nil {
				return 0, nil, nil, err
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
			return nil, err
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

func validateFolderNameAvailable(
	ctx context.Context,
	database executor,
	profileID, nameKey, excludedFolderID string,
) error {
	var count int
	if err := database.QueryRowContext(ctx, `
SELECT count(*)
FROM favorite_folders
WHERE profile_id=? AND name_key=? AND (?='' OR id<>?)
`, profileID, nameKey, excludedFolderID, excludedFolderID).Scan(&count); err != nil {
		return fmt.Errorf("favorites: check folder name: %w", err)
	}
	if count > 0 {
		return ErrFolderNameConflict
	}
	return nil
}

func folderByID(ctx context.Context, database executor, profileID, folderID string) (Folder, error) {
	var folder Folder
	err := database.QueryRowContext(ctx, `
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
		return Folder{}, ErrFolderNotFound
	}
	if err != nil {
		return Folder{}, fmt.Errorf("favorites: read folder: %w", err)
	}
	return folder, nil
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
		return IdempotentResponse{}, err
	}
	games := append([]string{}, initialGameIDs...)
	sort.Strings(games)
	request := struct {
		Name           string   `json:"name"`
		InitialGameIDs []string `json:"initialGameIds"`
	}{name, games}
	return service.idempotent(ctx, principal, "postFavoriteFolder", key, request,
		func(connection *sql.Conn) (int, map[string]string, any, error) {
			var count int
			if err := connection.QueryRowContext(ctx, `
SELECT count(*) FROM favorite_folders WHERE profile_id=?
`, principal.ProfileID).Scan(&count); err != nil {
				return 0, nil, nil, fmt.Errorf("favorites: count folders: %w", err)
			}
			if count >= MaxFolders {
				return 0, nil, nil, ErrFolderLimit
			}
			if err := validateFolderNameAvailable(ctx, connection, principal.ProfileID, nameKey, ""); err != nil {
				return 0, nil, nil, err
			}
			if err := validateVisibleGames(ctx, connection, games); err != nil {
				return 0, nil, nil, err
			}
			folderID, err := uuid.NewV7()
			if err != nil {
				return 0, nil, nil, fmt.Errorf("favorites: new folder id: %w", err)
			}
			now := service.now().UnixMilli()
			if _, err := connection.ExecContext(ctx, `
INSERT INTO favorite_folders(id,profile_id,name,name_key,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,1,?,?)
	`, folderID.String(), principal.ProfileID, name, nameKey, now, now); err != nil {
				return 0, nil, nil, fmt.Errorf("favorites: create folder: %w", err)
			}
			for _, gameID := range games {
				if _, err := connection.ExecContext(ctx, `
INSERT INTO favorite_games(profile_id,game_id,created_at_ms) VALUES(?,?,?)
ON CONFLICT(profile_id,game_id) DO NOTHING
`, principal.ProfileID, gameID, now); err != nil {
					return 0, nil, nil, fmt.Errorf("favorites: create folder favorite: %w", err)
				}
				if _, err := connection.ExecContext(ctx, `
INSERT INTO favorite_folder_games(profile_id,folder_id,game_id,created_at_ms) VALUES(?,?,?,?)
`, principal.ProfileID, folderID.String(), gameID, now); err != nil {
					return 0, nil, nil, fmt.Errorf("favorites: create folder membership: %w", err)
				}
			}
			folder, err := folderByID(ctx, connection, principal.ProfileID, folderID.String())
			if err != nil {
				return 0, nil, nil, err
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
		func(connection *sql.Conn) (int, map[string]string, any, error) {
			folder, err := folderByID(ctx, connection, principal.ProfileID, folderID)
			if err != nil {
				return 0, nil, nil, err
			}
			if folder.Version != expectedVersion {
				return 0, nil, nil, ErrVersionConflict
			}
			if folder.Name == name {
				return 0, nil, nil, ErrInvalid
			}
			if err := validateFolderNameAvailable(
				ctx, connection, principal.ProfileID, nameKey, folderID,
			); err != nil {
				return 0, nil, nil, err
			}
			now := service.now().UnixMilli()
			_, err = connection.ExecContext(ctx, `
UPDATE favorite_folders
SET name=?,name_key=?,version=version+1,updated_at_ms=?
WHERE profile_id=? AND id=? AND version=?
	`, name, nameKey, now, principal.ProfileID, folderID, expectedVersion)
			if err != nil {
				return 0, nil, nil, fmt.Errorf("favorites: rename folder: %w", err)
			}
			folder, err = folderByID(ctx, connection, principal.ProfileID, folderID)
			if err != nil {
				return 0, nil, nil, err
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
		func(connection *sql.Conn) (int, map[string]string, any, error) {
			folder, err := folderByID(ctx, connection, principal.ProfileID, folderID)
			if err != nil {
				return 0, nil, nil, err
			}
			if folder.Version != expectedVersion {
				return 0, nil, nil, ErrVersionConflict
			}
			if _, err := connection.ExecContext(ctx, `
DELETE FROM favorite_folder_games WHERE profile_id=? AND folder_id=?
`, principal.ProfileID, folderID); err != nil {
				return 0, nil, nil, fmt.Errorf("favorites: delete folder memberships: %w", err)
			}
			if _, err := connection.ExecContext(ctx, `
DELETE FROM favorite_folders WHERE profile_id=? AND id=? AND version=?
`, principal.ProfileID, folderID, expectedVersion); err != nil {
				return 0, nil, nil, fmt.Errorf("favorites: delete folder: %w", err)
			}
			return 204, map[string]string{}, nil, nil
		})
}
