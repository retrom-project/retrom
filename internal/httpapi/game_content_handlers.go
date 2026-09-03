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

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/gamecontent"
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

func (server *Server) createGameContentRevision(writer http.ResponseWriter, request *http.Request) {
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
		OperationID: "postAdminGameContentRevision",
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
	var instanceID, instanceName, platformID, contentID, metadataID string
	var players, releaseYear, deletedAt sql.NullInt64
	var releaseJobID, payloadError sql.NullString
	var version, createdAt, updatedAt int64
	err := server.database.QueryRowContext(request.Context(), `
SELECT m.title,
m.description,
m.developer,
m.publisher,
m.genre,
m.players,
m.release_year,
g.status,
g.payload_state,
g.payload_release_job_id,
g.payload_last_error_code,
pi.id,
pi.name,
pi.platform_id,
g.current_content_revision_id,
g.current_metadata_revision_id,
g.version,
g.created_at_ms,
g.updated_at_ms,
g.deleted_at_ms
FROM games g
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
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
			&contentID,
			&metadataID,
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
	metadataRevisions, err := server.gameMetadataRevisions(request.Context(), request.PathValue("gameId"), metadataID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	assets, err := server.adminGameAssets(request.Context(), request.PathValue("gameId"), metadataID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	contentRevisions, err := server.gameContentRevisions(
		request.Context(), request.PathValue("gameId"), contentID,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if status == "DELETED" {
		assets = []map[string]any{}
		for _, revision := range contentRevisions {
			revision["files"] = []map[string]any{}
		}
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
		"currentContentRevisionId": contentID, "currentMetadataRevisionId": metadataID, "version": version,
		"createdAtMs": createdAt, "updatedAtMs": updatedAt, "generatedAtMs": server.now().UnixMilli(),
		"deletedAtMs":       nullableInteger(deletedAt),
		"deleteImpact":      impact,
		"metadataRevisions": metadataRevisions, "assets": assets, "contentRevisions": contentRevisions,
		"variants": variants, "tags": tags,
	})
}

func (server *Server) gameContentRevisions(
	ctx context.Context,
	gameID, currentContentID string,
) ([]map[string]any, error) {
	contentRows, err := server.database.QueryContext(ctx, `
SELECT cr.id,
cr.source_kind,
cr.source_ref_id,
cr.content_kind,
cr.created_at_ms,
COALESCE((SELECT json_group_array(json_object(
'role', ordered.role,
'logicalName', ordered.logical_name,
'sortOrder', ordered.sort_order,
'sizeBytes', ordered.size_bytes,
'sha256', ordered.sha256))
FROM (SELECT role,
logical_name,
sort_order,
blob.size_bytes,
blob.sha256
FROM game_content_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.game_content_revision_id=cr.id
ORDER BY sort_order,
role,
logical_name) ordered), '[]')
FROM game_content_revisions cr
WHERE cr.game_id=?
ORDER BY cr.created_at_ms DESC,
cr.id DESC
`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query game content revisions: %w", err)
	}
	defer func() { cleanup.Error("close", contentRows.Close()) }()
	contentRevisions := make([]map[string]any, 0)
	for contentRows.Next() {
		var id, sourceKind, sourceRef, contentKind, filesJSON string
		var createdAtMS int64
		if err := contentRows.Scan(&id, &sourceKind, &sourceRef, &contentKind, &createdAtMS, &filesJSON); err != nil {
			return nil, fmt.Errorf("scan game content revision: %w", err)
		}
		files := make([]map[string]any, 0)
		if err := json.Unmarshal([]byte(filesJSON), &files); err != nil {
			return nil, fmt.Errorf("decode game content revision files: %w", err)
		}
		contentRevisions = append(
			contentRevisions,
			map[string]any{
				"id":          id,
				"sourceKind":  sourceKind,
				"sourceRefId": sourceRef,
				"contentKind": contentKind,
				"current":     id == currentContentID,
				"files":       files,
				"createdAtMs": createdAtMS,
			},
		)
	}
	if err := contentRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate game content revisions: %w", err)
	}
	return contentRevisions, nil
}

func (server *Server) gameMetadataRevisions(
	ctx context.Context,
	gameID, currentMetadataID string,
) ([]map[string]any, error) {
	metadataRows, err := server.database.QueryContext(ctx, `
SELECT id,
source_kind,
source_ref_id,
created_at_ms
FROM game_metadata_revisions
WHERE game_id=?
ORDER BY created_at_ms DESC,
id DESC
`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query game metadata revisions: %w", err)
	}
	defer func() { cleanup.Error("close", metadataRows.Close()) }()
	metadataRevisions := make([]map[string]any, 0)
	for metadataRows.Next() {
		var id, sourceKind string
		var sourceRef sql.NullString
		var createdAtMS int64
		if err := metadataRows.Scan(&id, &sourceKind, &sourceRef, &createdAtMS); err != nil {
			return nil, fmt.Errorf("scan game metadata revision: %w", err)
		}
		metadataRevisions = append(
			metadataRevisions,
			map[string]any{
				"id":          id,
				"sourceKind":  sourceKind,
				"sourceRefId": nullableString(sourceRef),
				"current":     id == currentMetadataID,
				"createdAtMs": createdAtMS,
			},
		)
	}
	if err := metadataRows.Err(); err != nil {
		return nil, fmt.Errorf("scan game metadata revisions: %w", err)
	}
	return metadataRevisions, nil
}

func (server *Server) adminGameAssets(
	ctx context.Context,
	gameID, metadataID string,
) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT id,kind,ordinal,width_px,height_px,media_type,created_at_ms
FROM game_assets
WHERE game_id=? AND metadata_revision_id=?
ORDER BY kind,ordinal,id
`, gameID, metadataID)
	if err != nil {
		return nil, fmt.Errorf("query admin game assets: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	assets := make([]map[string]any, 0)
	for rows.Next() {
		var id, kind, mediaType string
		var ordinal, createdAtMS int64
		var width, height sql.NullInt64
		if err := rows.Scan(&id, &kind, &ordinal, &width, &height, &mediaType, &createdAtMS); err != nil {
			return nil, fmt.Errorf("scan admin game asset: %w", err)
		}
		assets = append(assets, map[string]any{
			"assetId": id, "kind": kind, "ordinal": ordinal,
			"widthPx": nullableInteger(width), "heightPx": nullableInteger(height),
			"mediaType": mediaType, "url": "/content/assets/" + id, "createdAtMs": createdAtMS,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin game assets: %w", err)
	}
	return assets, nil
}

func (server *Server) adminGameVariants(ctx context.Context, gameID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT v.id,v.core_id,c.name,v.current_revision_id,v.version,
COALESCE((SELECT json_group_array(json_object(
'id', ordered.id,'contentRevisionId', ordered.game_content_revision_id,
'providerId', ordered.provider_id,'targetId',ordered.target_id,
'targetContractSha256',ordered.target_contract_sha256,
'gameCompatibilityLine',ordered.game_compatibility_line,'datVersionId', ordered.dat_version_id,
'status', ordered.status,'compatibilityCode', ordered.compatibility_code,
'dependencySnapshot', json(ordered.dependency_snapshot_json),
'current', ordered.id=v.current_revision_id,'createdAtMs', ordered.created_at_ms))
FROM (SELECT id,game_content_revision_id,provider_id,target_id,target_contract_sha256,
game_compatibility_line,dat_version_id,status,compatibility_code,
dependency_snapshot_json,created_at_ms
FROM game_variant_revisions WHERE game_variant_id=v.id
ORDER BY created_at_ms DESC,id DESC) ordered), '[]')
FROM game_variants v
JOIN cores c ON c.id=v.core_id
WHERE v.game_id=?
ORDER BY c.name,v.id
`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query admin game variants: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	variants := make([]map[string]any, 0)
	for rows.Next() {
		var id, coreID, coreName, revisionsJSON string
		var currentRevision sql.NullString
		var version int64
		if err := rows.Scan(&id, &coreID, &coreName, &currentRevision, &version, &revisionsJSON); err != nil {
			return nil, fmt.Errorf("scan admin game variant: %w", err)
		}
		revisions := make([]map[string]any, 0)
		if err := json.Unmarshal([]byte(revisionsJSON), &revisions); err != nil {
			return nil, fmt.Errorf("decode admin game variant revisions: %w", err)
		}
		variants = append(variants, map[string]any{
			"id": id, "coreId": coreID, "coreName": coreName,
			"currentRevisionId": nullableString(currentRevision), "version": version, "revisions": revisions,
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

func copyGameMetadataAssets(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, currentMetadataID, revisionID string,
	now int64,
) error {
	assetRows, err := transaction.QueryContext(ctx, `
SELECT blob_id,kind,ordinal,width_px,height_px,media_type
FROM game_assets
WHERE game_id=? AND metadata_revision_id=?
ORDER BY kind,ordinal
`, gameID, currentMetadataID)
	if err != nil {
		return fmt.Errorf("query current game assets: %w", err)
	}
	defer func() { cleanup.Error("close", assetRows.Close()) }()
	for assetRows.Next() {
		var blobID, kind, mediaType string
		var ordinal int64
		var width, height sql.NullInt64
		if err := assetRows.Scan(&blobID, &kind, &ordinal, &width, &height, &mediaType); err != nil {
			return fmt.Errorf("scan current game asset: %w", err)
		}
		assetID, _ := uuid.NewV7()
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_assets(
id,game_id,metadata_revision_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?)
`, assetID.String(), gameID, revisionID, blobID, kind, ordinal, nullableInteger(width),
			nullableInteger(height), mediaType, now); err != nil {
			return fmt.Errorf("copy current game asset: %w", err)
		}
	}
	if err := assetRows.Err(); err != nil {
		return fmt.Errorf("iterate current game assets: %w", err)
	}
	return nil
}

type patchGameState struct {
	metadataID, status string
	version            int64
	metadata           gameMetadata
}

func loadPatchGameState(ctx context.Context, transaction *sql.Tx, gameID string) (patchGameState, error) {
	var state patchGameState
	err := transaction.QueryRowContext(ctx, `
SELECT g.current_metadata_revision_id,
g.status,
g.version,
m.title,
m.description,
m.developer,
m.publisher,
m.genre,
m.players,
m.release_year
FROM games g
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
WHERE g.id=?
`, gameID).Scan(
		&state.metadataID, &state.status, &state.version, &state.metadata.Title, &state.metadata.Description,
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
	revisionID, _ := uuid.NewV7()
	now := server.now().UnixMilli()
	if err := insertAdminGameMetadataRevision(
		request.Context(), transaction, revisionID.String(), request.PathValue("gameId"), state.metadata, now,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := copyGameMetadataAssets(
		request.Context(), transaction, request.PathValue("gameId"), state.metadataID, revisionID.String(), now,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	search := strings.ToLower(
		strings.Join(
			[]string{state.metadata.Title, state.metadata.Developer, state.metadata.Publisher, state.metadata.Genre}, " ",
		),
	)
	result, err := transaction.ExecContext(
		request.Context(),
		`
UPDATE games
SET current_metadata_revision_id=?,
search_text=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?
`,
		revisionID.String(),
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
	if err := server.retireSupersededGameAssets(
		request.Context(), transaction, request.PathValue("gameId"), revisionID.String(),
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := insertAudit(
		request,
		transaction,
		"GAME_METADATA_UPDATED",
		"GAME",
		request.PathValue("gameId"),
		map[string]any{"metadataRevisionId": state.metadataID},
		map[string]any{"metadataRevisionId": revisionID.String()},
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
			"gameId":             request.PathValue("gameId"),
			"metadataRevisionId": revisionID.String(),
			"version":            expected + 1,
			"updatedAtMs":        now,
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
