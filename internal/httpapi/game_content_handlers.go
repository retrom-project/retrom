package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/gamecontent"
	"retrom/internal/gametitle"
	"retrom/internal/payloadrelease"
)

type gameMetadata struct {
	Title       string
	Description string
	Developer   string
	Publisher   string
	Genre       string
	Players     sql.NullInt64
	ReleaseYear sql.NullInt64
}

type patchGameRequest struct {
	Title       *string               `json:"title,omitempty"`
	Description *string               `json:"description,omitempty"`
	Developer   *string               `json:"developer,omitempty"`
	Publisher   *string               `json:"publisher,omitempty"`
	Genre       *string               `json:"genre,omitempty"`
	Players     optionalNullableInt64 `json:"players,omitempty"`
	ReleaseYear optionalNullableInt64 `json:"releaseYear,omitempty"`
}

type optionalNullableInt64 struct {
	Present bool
	Value   *int64
}

func resolvedContentMode(value string) string {
	if value == "" {
		return "STANDARD"
	}
	return value
}

func (value *optionalNullableInt64) UnmarshalJSON(contents []byte) error {
	value.Present = true
	if string(contents) == "null" {
		value.Value = nil
		return nil
	}
	var decoded int64
	if err := json.Unmarshal(contents, &decoded); err != nil {
		return fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	value.Value = &decoded
	return nil
}

func (server *Server) createGameContentReplacement(writer http.ResponseWriter, request *http.Request) {
	expected, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct {
		UploadID    string `json:"uploadId"`
		ContentMode string `json:"contentMode"`
	}
	if err := decodeJSON(writer, request, &body, 4096); err != nil || strings.TrimSpace(body.UploadID) == "" {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "内容替换上传无效", map[string]any{})
		return
	}
	canonical, _ := json.Marshal(struct {
		OperationID string `json:"operationId"`
		GameID      string `json:"gameId"`
		IfMatch     int64  `json:"ifMatch"`
		MediaType   string `json:"mediaType"`
		UploadID    string `json:"uploadId"`
		ContentMode string `json:"contentMode"`
	}{
		OperationID: "postAdminGameContentReplacement",
		GameID:      request.PathValue("gameId"),
		IfMatch:     expected,
		MediaType:   "application/json",
		UploadID:    body.UploadID,
		ContentMode: resolvedContentMode(body.ContentMode),
	})
	digest := sha256.Sum256(canonical)
	scheduled, replayed, err := server.gameContent.ScheduleIdempotentMode(
		request.Context(),
		request.PathValue("gameId"),
		body.UploadID,
		resolvedContentMode(body.ContentMode),
		expected,
		request.Header.Get("Idempotency-Key"),
		hex.EncodeToString(digest[:]),
	)
	if errors.Is(err, gamecontent.ErrIdempotencyKeyReused) {
		writeError(writer, request, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "幂等键已用于另一请求", map[string]any{})
		return
	}
	if errors.Is(err, gamecontent.ErrInvalid) {
		writeError(
			writer,
			request,
			http.StatusConflict,
			"GAME_CONTENT_REPLACEMENT_CONFLICT",
			"游戏、目录或上传状态已经变化",
			map[string]any{},
		)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, scheduled.Version))
	if replayed {
		writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
	}
	writeJSON(writer, http.StatusAccepted, scheduled)
}

// Contract branches stay contiguous for a single auditable decision.
func (server *Server) adminGame(writer http.ResponseWriter, request *http.Request) {
	var title, description, developer, publisher, genre, status, payloadState string
	var instanceID, instanceName, platformID, contentKind string
	var players, releaseYear, deletedAt sql.NullInt64
	var releaseJobID, payloadError sql.NullString
	var version, createdAt, updatedAt int64
	err := server.database.QueryRowContext(request.Context(), `
SELECT g.title,
g.description,
g.developer,
g.publisher,
g.genre,
g.players,
g.release_year,
g.status,
g.payload_state,
g.payload_release_job_id,
g.payload_last_error_code,
pi.id,
pi.name,
pi.platform_id,
g.content_kind,
g.version,
g.created_at_ms,
g.updated_at_ms,
g.deleted_at_ms
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE g.id=?
`, request.PathValue("gameId")).
		Scan(
			&title,
			&description,
			&developer,
			&publisher,
			&genre,
			&players,
			&releaseYear,
			&status,
			&payloadState,
			&releaseJobID,
			&payloadError,
			&instanceID,
			&instanceName,
			&platformID,
			&contentKind,
			&version,
			&createdAt,
			&updatedAt,
			&deletedAt,
		)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(writer, request, http.StatusNotFound, "GAME_NOT_FOUND", "游戏不存在", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	impact, err := payloadrelease.GameDeleteImpact(request.Context(), server.database, request.PathValue("gameId"))
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	assets, err := server.adminGameAssets(request.Context(), request.PathValue("gameId"))
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	files, err := server.adminGameFiles(request.Context(), request.PathValue("gameId"))
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if status == "DELETED" {
		assets = []map[string]any{}
		files = []map[string]any{}
	}
	variants, err := server.adminGameVariants(request.Context(), request.PathValue("gameId"))
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	tags, err := server.activeGameTags(request.Context(), request.PathValue("gameId"))
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version))
	writeJSON(writer, http.StatusOK, map[string]any{
		"gameId": request.PathValue(
			"gameId",
		), "status": status, "payloadState": payloadState,
		"payloadReleaseJobId": nullableString(releaseJobID), "payloadLastErrorCode": nullableString(payloadError),
		"title": title, "description": description, "developer": developer,
		"publisher": publisher, "genre": genre,
		"players": nullableInteger(players), "releaseYear": nullableInteger(releaseYear),
		"platformId": platformID, "platformInstance": map[string]any{"id": instanceID, "name": instanceName},
		"contentKind": contentKind, "files": files, "version": version,
		"createdAtMs": createdAt, "updatedAtMs": updatedAt, "generatedAtMs": server.now().UnixMilli(),
		"deletedAtMs":  nullableInteger(deletedAt),
		"deleteImpact": impact, "assets": assets,
		"variants": variants, "tags": tags,
	})
}

func (server *Server) adminGameFiles(ctx context.Context, gameID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT file.role,file.logical_name,file.sort_order,blob.size_bytes,blob.sha256
FROM game_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.game_id=?
ORDER BY file.sort_order,file.role,file.logical_name
`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query admin game files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]map[string]any, 0)
	for rows.Next() {
		var role, logicalName, sha256 string
		var sortOrder, sizeBytes int64
		if err := rows.Scan(&role, &logicalName, &sortOrder, &sizeBytes, &sha256); err != nil {
			return nil, fmt.Errorf("scan admin game file: %w", err)
		}
		files = append(files, map[string]any{
			"role": role, "logicalName": logicalName, "sortOrder": sortOrder,
			"sizeBytes": sizeBytes, "sha256": sha256,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin game files: %w", err)
	}
	return files, nil
}

func (server *Server) adminGameAssets(ctx context.Context, gameID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT id,kind,ordinal,width_px,height_px,media_type
FROM game_assets
WHERE game_id=?
ORDER BY kind,ordinal,id
`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query admin game assets: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	assets := make([]map[string]any, 0)
	for rows.Next() {
		var id, kind, mediaType string
		var ordinal int64
		var width, height sql.NullInt64
		if err := rows.Scan(&id, &kind, &ordinal, &width, &height, &mediaType); err != nil {
			return nil, fmt.Errorf("scan admin game asset: %w", err)
		}
		assets = append(assets, map[string]any{
			"assetId": id, "kind": kind, "ordinal": ordinal,
			"widthPx": nullableInteger(width), "heightPx": nullableInteger(height),
			"mediaType": mediaType, "url": "/content/assets/" + id,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin game assets: %w", err)
	}
	return assets, nil
}

func (server *Server) adminGameVariants(ctx context.Context, gameID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT variant.id,variant.core_id,core.name,variant.provider_id,variant.target_id,
 variant.dat_version_id,variant.status,variant.compatibility_code,variant.dependency_snapshot_json,
 variant.version,variant.created_at_ms,variant.updated_at_ms
FROM game_variants variant
JOIN cores core ON core.id=variant.core_id
WHERE variant.game_id=?
ORDER BY core.name,variant.id
`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query admin game variants: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	variants := make([]map[string]any, 0)
	for rows.Next() {
		var id, coreID, coreName, status, compatibilityCode, dependencyJSON string
		var providerID, targetID, datVersionID sql.NullString
		var version, createdAtMS, updatedAtMS int64
		if err := rows.Scan(
			&id, &coreID, &coreName, &providerID, &targetID, &datVersionID, &status,
			&compatibilityCode, &dependencyJSON, &version, &createdAtMS, &updatedAtMS,
		); err != nil {
			return nil, fmt.Errorf("scan admin game variant: %w", err)
		}
		dependencySnapshot := make(map[string]any)
		if err := json.Unmarshal([]byte(dependencyJSON), &dependencySnapshot); err != nil {
			return nil, fmt.Errorf("decode admin game variant dependency snapshot: %w", err)
		}
		variants = append(variants, map[string]any{
			"id": id, "coreId": coreID, "coreName": coreName,
			"providerId": nullableString(providerID), "targetId": nullableString(targetID),
			"datVersionId": nullableString(datVersionID), "status": status,
			"compatibilityCode": compatibilityCode, "dependencySnapshot": dependencySnapshot,
			"version": version, "createdAtMs": createdAtMS, "updatedAtMs": updatedAtMS,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin game variants: %w", err)
	}
	return variants, nil
}

// Each branch is an independent DTO constraint retained as the single patch validation source.
func validPatchGame(body patchGameRequest, now time.Time) bool {
	if !hasPatchGameChanges(body) {
		return false
	}
	return validPatchGameText(body) && validPatchGameNumbers(body, now)
}

func hasPatchGameChanges(body patchGameRequest) bool {
	return body.Title != nil || body.Description != nil || body.Developer != nil || body.Publisher != nil ||
		body.Genre != nil || body.Players.Present || body.ReleaseYear.Present
}

func validPatchGameText(body patchGameRequest) bool {
	return (body.Title == nil || validText(*body.Title, 1, 200, false)) &&
		(body.Description == nil || validText(*body.Description, 0, 10_000, true)) &&
		(body.Developer == nil || validText(*body.Developer, 0, 200, false)) &&
		(body.Publisher == nil || validText(*body.Publisher, 0, 200, false)) &&
		(body.Genre == nil || validText(*body.Genre, 0, 200, false))
}

func validPatchGameNumbers(body patchGameRequest, now time.Time) bool {
	return (!body.Players.Present || body.Players.Value == nil || *body.Players.Value >= 1 && *body.Players.Value <= 64) &&
		(!body.ReleaseYear.Present || body.ReleaseYear.Value == nil ||
			*body.ReleaseYear.Value >= 1950 && *body.ReleaseYear.Value <= int64(now.UTC().Year()+1))
}

func applyPatchGameMetadata(metadata *gameMetadata, body patchGameRequest) {
	if body.Title != nil {
		metadata.Title = *body.Title
	}
	if body.Description != nil {
		metadata.Description = *body.Description
	}
	if body.Developer != nil {
		metadata.Developer = *body.Developer
	}
	if body.Publisher != nil {
		metadata.Publisher = *body.Publisher
	}
	if body.Genre != nil {
		metadata.Genre = *body.Genre
	}
	if body.Players.Present {
		metadata.Players = sql.NullInt64{}
		if body.Players.Value != nil {
			metadata.Players = sql.NullInt64{Int64: *body.Players.Value, Valid: true}
		}
	}
	if body.ReleaseYear.Present {
		metadata.ReleaseYear = sql.NullInt64{}
		if body.ReleaseYear.Value != nil {
			metadata.ReleaseYear = sql.NullInt64{Int64: *body.ReleaseYear.Value, Valid: true}
		}
	}
}

type patchGameState struct {
	status   string
	version  int64
	metadata gameMetadata
}

func loadPatchGameState(ctx context.Context, transaction *sql.Tx, gameID string) (patchGameState, error) {
	var state patchGameState
	err := transaction.QueryRowContext(ctx, `
SELECT g.status,
g.version,
g.title,
g.description,
g.developer,
g.publisher,
g.genre,
g.players,
g.release_year
FROM games g
WHERE g.id=?
`, gameID).Scan(
		&state.status, &state.version, &state.metadata.Title, &state.metadata.Description,
		&state.metadata.Developer, &state.metadata.Publisher, &state.metadata.Genre, &state.metadata.Players,
		&state.metadata.ReleaseYear,
	)
	if err != nil {
		return patchGameState{}, fmt.Errorf("load patch game state: %w", err)
	}
	return state, nil
}

// Contract branches stay contiguous for a single auditable decision.
func (server *Server) patchAdminGame(writer http.ResponseWriter, request *http.Request) {
	expected, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusPreconditionRequired,
			"PRECONDITION_REQUIRED",
			"需要当前资源版本",
			map[string]any{},
		)
		return
	}
	var body patchGameRequest
	if err := decodeJSON(writer, request, &body, 32<<10); err != nil || !validPatchGame(body, server.now()) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "游戏元信息无效", map[string]any{})
		return
	}
	transaction, err := server.database.BeginTx(request.Context(), nil)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer cleanup.Rollback(transaction)
	state, err := loadPatchGameState(request.Context(), transaction, request.PathValue("gameId"))
	if err != nil {
		writeError(writer, request, http.StatusNotFound, "GAME_NOT_FOUND", "游戏不存在", map[string]any{})
		return
	}
	if state.version != expected || state.status != "PUBLISHED" {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	applyPatchGameMetadata(&state.metadata, body)
	now := server.now().UnixMilli()
	search := strings.ToLower(
		strings.Join(
			[]string{state.metadata.Title, state.metadata.Developer, state.metadata.Publisher, state.metadata.Genre}, " ",
		),
	)
	result, err := transaction.ExecContext(
		request.Context(),
		`
UPDATE games
SET title=?,title_initial=?,description=?,developer=?,publisher=?,genre=?,players=?,release_year=?,
metadata_source_kind='ADMIN_EDIT',metadata_source_ref_id=NULL,search_text=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?
`,
		state.metadata.Title,
		gametitle.Initial(state.metadata.Title),
		state.metadata.Description,
		state.metadata.Developer,
		state.metadata.Publisher,
		state.metadata.Genre,
		nullableInteger(state.metadata.Players),
		nullableInteger(state.metadata.ReleaseYear),
		search,
		now,
		request.PathValue("gameId"),
		expected,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if err := insertAudit(
		request,
		transaction,
		"GAME_METADATA_UPDATED",
		"GAME",
		request.PathValue("gameId"),
		map[string]any{"version": expected},
		map[string]any{"version": expected + 1},
		now,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	server.payloadReleases.Signal()
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected+1))
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"gameId": request.PathValue("gameId"), "version": expected + 1,
		},
	)
}

// Delete preconditions, reference checks, optimistic locking, and audit write share one transaction.
func (server *Server) deleteAdminGame(writer http.ResponseWriter, request *http.Request) {
	input, valid := parseDeleteGameInput(writer, request)
	if !valid {
		return
	}
	server.lockIdempotentRequest()
	defer server.idempotency.Unlock()
	transaction, err := server.database.BeginTx(request.Context(), nil)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer cleanup.Rollback(transaction)
	now := server.now().UnixMilli()
	if server.replayDeleteGameIfPresent(writer, request, transaction, input, now) {
		return
	}
	state, err := loadDeleteGameState(request.Context(), transaction, request.PathValue("gameId"))
	if err != nil {
		writeError(writer, request, http.StatusNotFound, "GAME_NOT_FOUND", "游戏不存在", map[string]any{})
		return
	}
	if server.respondToExistingGameTombstone(writer, request, transaction, input, state, now) {
		return
	}
	impact, valid := server.validateDeleteGameImpact(writer, request, transaction, input, state)
	if !valid {
		return
	}
	releaseJob, err := payloadrelease.ScheduleGameDeletion(
		request.Context(), transaction, request.PathValue("gameId"), input.expected, now,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := transitionDeletedGameRuntime(request.Context(), transaction, request.PathValue("gameId"), now); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := insertAudit(
		request,
		transaction,
		"GAME_PERMANENT_DELETE_REQUESTED",
		"GAME",
		request.PathValue("gameId"),
		map[string]any{"status": "PUBLISHED"},
		map[string]any{
			"status": "DELETED", "payloadState": "RELEASING", "payloadReleaseJobId": releaseJob,
			"impact": payloadrelease.GameDeleteAuditImpact(impact),
		},
		now,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	response := deleteGameResponse{
		GameID: request.PathValue("gameId"), Status: "DELETED", PayloadState: "RELEASING",
		PayloadReleaseJobID: &releaseJob,
	}
	etag := fmt.Sprintf(`"v%d"`, input.expected+1)
	responseBody, err := storeDeleteGameResponse(
		request.Context(), transaction, input.principal.UserID, request.Header.Get("Idempotency-Key"),
		input.requestDigest, etag, http.StatusAccepted, response, now,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	server.payloadReleases.Signal()
	writeStoredJSON(writer, http.StatusAccepted, etag, responseBody)
}
