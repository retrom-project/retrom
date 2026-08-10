package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/gamecontent"
	"retrom/internal/hasheous"
	"retrom/internal/libraryimport"
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

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (server *Server) adminGame(writer http.ResponseWriter, request *http.Request) {
	var title, description, developer, publisher, genre, status string
	var instanceID, instanceName, platformID, contentID, metadataID string
	var players, releaseYear, deletedAt sql.NullInt64
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
	var saveCount, reviewCount, launchCount int64
	_ = server.database.QueryRowContext(request.Context(), `
SELECT count(*)
FROM save_states
WHERE game_id=?
AND deleted_at_ms IS NULL
`, request.PathValue("gameId")).
		Scan(&saveCount)
	_ = server.database.QueryRowContext(request.Context(), `
SELECT count(*)
FROM review_events
WHERE json_extract(after_json,
'$.gameId')=?
`, request.PathValue("gameId")).
		Scan(&reviewCount)
	_ = server.database.QueryRowContext(request.Context(), `
SELECT count(*)
FROM launch_sessions
WHERE game_id=?
AND state IN ('CREATED',
'ACTIVE')
`, request.PathValue("gameId")).
		Scan(&launchCount)
	metadataRevisions := make([]map[string]any, 0)
	metadataRows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT id,
source_kind,
source_ref_id,
created_at_ms
FROM game_metadata_revisions
WHERE game_id=?
ORDER BY created_at_ms DESC,
id DESC
`,
		request.PathValue("gameId"),
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", metadataRows.Close()) }()
	for metadataRows.Next() {
		var id, sourceKind string
		var sourceRef sql.NullString
		var createdAtMS int64
		if err := metadataRows.Scan(&id, &sourceKind, &sourceRef, &createdAtMS); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		metadataRevisions = append(
			metadataRevisions,
			map[string]any{
				"id":          id,
				"sourceKind":  sourceKind,
				"sourceRefId": nullableString(sourceRef),
				"current":     id == metadataID,
				"createdAtMs": createdAtMS,
			},
		)
	}
	if err := metadataRows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	assets := make([]map[string]any, 0)
	assetRows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT id,
kind,
ordinal,
width_px,
height_px,
media_type,
created_at_ms
FROM game_assets
WHERE game_id=?
AND metadata_revision_id=?
ORDER BY kind,
ordinal,
id
`,
		request.PathValue("gameId"),
		metadataID,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", assetRows.Close()) }()
	for assetRows.Next() {
		var id, kind, mediaType string
		var ordinal, width, height, createdAtMS int64
		if err := assetRows.Scan(&id, &kind, &ordinal, &width, &height, &mediaType, &createdAtMS); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		assets = append(
			assets,
			map[string]any{
				"assetId":     id,
				"kind":        kind,
				"ordinal":     ordinal,
				"widthPx":     width,
				"heightPx":    height,
				"mediaType":   mediaType,
				"url":         "/content/assets/" + id,
				"createdAtMs": createdAtMS,
			},
		)
	}
	if err := assetRows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	contentRevisions := make([]map[string]any, 0)
	contentRows, err := server.database.QueryContext(
		request.Context(),
		`
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
`,
		request.PathValue("gameId"),
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", contentRows.Close()) }()
	for contentRows.Next() {
		var id, sourceKind, sourceRef, contentKind, filesJSON string
		var createdAtMS int64
		if err := contentRows.Scan(&id, &sourceKind, &sourceRef, &contentKind, &createdAtMS, &filesJSON); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		files := make([]map[string]any, 0)
		if err := json.Unmarshal([]byte(filesJSON), &files); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		contentRevisions = append(
			contentRevisions,
			map[string]any{
				"id":          id,
				"sourceKind":  sourceKind,
				"sourceRefId": sourceRef,
				"contentKind": contentKind,
				"current":     id == contentID,
				"files":       files,
				"createdAtMs": createdAtMS,
			},
		)
	}
	if err := contentRows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	variants := make([]map[string]any, 0)
	variantRows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT v.id,
v.core_id,
c.name,
v.current_revision_id,
v.version,
COALESCE((SELECT json_group_array(json_object(
'id', ordered.id,
'contentRevisionId', ordered.game_content_revision_id,
'coreArtifactId', ordered.core_artifact_id,
'datVersionId', ordered.dat_version_id,
'status', ordered.status,
'compatibilityCode', ordered.compatibility_code,
'dependencySnapshot', json(ordered.dependency_snapshot_json),
'current', ordered.id=v.current_revision_id,
'createdAtMs', ordered.created_at_ms))
FROM (SELECT id,
game_content_revision_id,
core_artifact_id,
dat_version_id,
status,
compatibility_code,
dependency_snapshot_json,
created_at_ms
FROM game_variant_revisions
WHERE game_variant_id=v.id
ORDER BY created_at_ms DESC,
id DESC) ordered), '[]')
FROM game_variants v
JOIN cores c ON c.id=v.core_id
WHERE v.game_id=?
ORDER BY c.name,
v.id
`,
		request.PathValue("gameId"),
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", variantRows.Close()) }()
	for variantRows.Next() {
		var id, coreID, coreName, revisionsJSON string
		var currentRevision sql.NullString
		var variantVersion int64
		if err := variantRows.Scan(
			&id,
			&coreID,
			&coreName,
			&currentRevision,
			&variantVersion,
			&revisionsJSON,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		revisions := make([]map[string]any, 0)
		if err := json.Unmarshal([]byte(revisionsJSON), &revisions); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		variants = append(
			variants,
			map[string]any{
				"id":                id,
				"coreId":            coreID,
				"coreName":          coreName,
				"currentRevisionId": nullableString(currentRevision),
				"version":           variantVersion,
				"revisions":         revisions,
			},
		)
	}
	if err := variantRows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version))
	writeJSON(writer, http.StatusOK, map[string]any{
		"gameId": request.PathValue(
			"gameId",
		), "status": status, "title": title, "description": description, "developer": developer,
		"publisher": publisher, "genre": genre,
		"players": nullableInteger(players), "releaseYear": nullableInteger(releaseYear),
		"platformId": platformID, "platformInstance": map[string]any{"id": instanceID, "name": instanceName},
		"currentContentRevisionId": contentID, "currentMetadataRevisionId": metadataID, "version": version,
		"createdAtMs": createdAt, "updatedAtMs": updatedAt, "generatedAtMs": server.now().UnixMilli(),
		"deletedAtMs": nullableInteger(deletedAt),
		"deleteImpact": map[string]any{
			"saveStateCount":    saveCount,
			"reviewEventCount":  reviewCount,
			"activeLaunchCount": launchCount,
		},
		"metadataRevisions": metadataRevisions, "assets": assets, "contentRevisions": contentRevisions, "variants": variants,
	})
}

//nolint:gocyclo // Each branch is an independent DTO constraint retained as the single patch validation source.
func validPatchGame(body patchGameRequest, now time.Time) bool {
	if body.Title == nil && body.Description == nil && body.Developer == nil && body.Publisher == nil &&
		body.Genre == nil &&
		!body.Players.Present &&
		!body.ReleaseYear.Present {
		return false
	}
	return (body.Title == nil || validText(*body.Title, 1, 200, false)) &&
		(body.Description == nil || validText(*body.Description, 0, 10_000, true)) &&
		(body.Developer == nil || validText(*body.Developer, 0, 200, false)) &&
		(body.Publisher == nil || validText(*body.Publisher, 0, 200, false)) &&
		(body.Genre == nil || validText(*body.Genre, 0, 200, false)) &&
		(!body.Players.Present || body.Players.Value == nil || *body.Players.Value >= 1 && *body.Players.Value <= 64) &&
		(!body.ReleaseYear.Present || body.ReleaseYear.Value == nil ||
			*body.ReleaseYear.Value >= 1950 && *body.ReleaseYear.Value <= int64(now.UTC().Year()+1))
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
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
	var current gameMetadata
	var currentMetadataID, status string
	var version int64
	err = transaction.QueryRowContext(request.Context(), `
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
`, request.PathValue("gameId")).
		Scan(
			&currentMetadataID,
			&status,
			&version,
			&current.Title,
			&current.Description,
			&current.Developer,
			&current.Publisher,
			&current.Genre,
			&current.Players,
			&current.ReleaseYear,
		)
	if err != nil {
		writeError(writer, request, http.StatusNotFound, "GAME_NOT_FOUND", "游戏不存在", map[string]any{})
		return
	}
	if version != expected || status != "PUBLISHED" {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if body.Title != nil {
		current.Title = *body.Title
	}
	if body.Description != nil {
		current.Description = *body.Description
	}
	if body.Developer != nil {
		current.Developer = *body.Developer
	}
	if body.Publisher != nil {
		current.Publisher = *body.Publisher
	}
	if body.Genre != nil {
		current.Genre = *body.Genre
	}
	if body.Players.Present {
		current.Players = sql.NullInt64{}
		if body.Players.Value != nil {
			current.Players = sql.NullInt64{Int64: *body.Players.Value, Valid: true}
		}
	}
	if body.ReleaseYear.Present {
		current.ReleaseYear = sql.NullInt64{}
		if body.ReleaseYear.Value != nil {
			current.ReleaseYear = sql.NullInt64{Int64: *body.ReleaseYear.Value, Valid: true}
		}
	}
	revisionID, _ := uuid.NewV7()
	now := server.now().UnixMilli()
	_, err = transaction.ExecContext(
		request.Context(),
		`
INSERT INTO game_metadata_revisions(id,
game_id,
title,
description,
developer,
publisher,
genre,
players,
release_year,
source_kind,
source_ref_id,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
'ADMIN_EDIT',
NULL,
?)
`,
		revisionID.String(),
		request.PathValue("gameId"),
		current.Title,
		current.Description,
		current.Developer,
		current.Publisher,
		current.Genre,
		nullableInteger(current.Players),
		nullableInteger(current.ReleaseYear),
		now,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	assetRows, err := transaction.QueryContext(
		request.Context(),
		`
SELECT blob_id,
kind,
ordinal,
width_px,
height_px,
media_type
FROM game_assets
WHERE game_id=?
AND metadata_revision_id=?
ORDER BY kind,
ordinal
`,
		request.PathValue("gameId"),
		currentMetadataID,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", assetRows.Close()) }()
	for assetRows.Next() {
		var blobID, kind, mediaType string
		var ordinal, width, height int64
		if err := assetRows.Scan(&blobID, &kind, &ordinal, &width, &height, &mediaType); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		assetID, _ := uuid.NewV7()
		if _, err := transaction.ExecContext(request.Context(), `
INSERT INTO game_assets(id,
game_id,
metadata_revision_id,
blob_id,
kind,
ordinal,
width_px,
height_px,
media_type,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
			assetID.String(),
			request.PathValue("gameId"),
			revisionID.String(),
			blobID,
			kind,
			ordinal,
			width,
			height,
			mediaType,
			now,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
	}
	if err := assetRows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	search := strings.ToLower(
		strings.Join([]string{current.Title, current.Developer, current.Publisher, current.Genre}, " "),
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
	if err := insertAudit(
		request,
		transaction,
		"GAME_METADATA_UPDATED",
		"GAME",
		request.PathValue("gameId"),
		map[string]any{"metadataRevisionId": currentMetadataID},
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

//nolint:funlen // Delete preconditions, reference checks, optimistic locking, and audit write share one transaction.
func (server *Server) deleteAdminGame(writer http.ResponseWriter, request *http.Request) {
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
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
	var body struct {
		ConfirmTitle string `json:"confirmTitle"`
	}
	if err := decodeJSON(writer, request, &body, 4096); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "删除确认无效", map[string]any{})
		return
	}
	transaction, err := server.database.BeginTx(request.Context(), nil)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer cleanup.Rollback(transaction)
	var title, status string
	var version int64
	if err := transaction.QueryRowContext(request.Context(), `
SELECT m.title,
g.status,
g.version
FROM games g
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
WHERE g.id=?
`, request.PathValue("gameId")).Scan(&title, &status, &version); err != nil {
		writeError(writer, request, http.StatusNotFound, "GAME_NOT_FOUND", "游戏不存在", map[string]any{})
		return
	}
	if status != "PUBLISHED" {
		writeError(writer, request, http.StatusConflict, "GAME_ALREADY_DELETED", "游戏已删除", map[string]any{})
		return
	}
	if version != expected {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if body.ConfirmTitle != title {
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"GAME_DELETE_CONFIRMATION_MISMATCH",
			"确认标题不匹配",
			map[string]any{},
		)
		return
	}
	now := server.now().UnixMilli()
	if _, err := transaction.ExecContext(request.Context(), `
UPDATE games
SET status='DELETED',
deleted_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?
`, now, now, request.PathValue("gameId"), expected); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if _, err := transaction.ExecContext(request.Context(), `
UPDATE launch_sessions
SET state='REVOKED',
finished_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE game_id=?
AND state IN ('CREATED',
'ACTIVE')
`, now, now, request.PathValue("gameId")); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := insertAudit(
		request,
		transaction,
		"GAME_DELETED",
		"GAME",
		request.PathValue("gameId"),
		map[string]any{"status": "PUBLISHED"},
		map[string]any{"status": "DELETED"},
		now,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected+1))
	writer.WriteHeader(http.StatusNoContent)
}

type gameMoveImpact struct {
	Action                   string   `json:"action"`
	GameID                   string   `json:"gameId"`
	GameVersion              int64    `json:"gameVersion"`
	ContentRevisionID        string   `json:"contentRevisionId"`
	SourcePlatformInstanceID string   `json:"sourcePlatformInstanceId"`
	TargetPlatformInstanceID string   `json:"targetPlatformInstanceId"`
	TargetPlatformVersion    int64    `json:"targetPlatformInstanceVersion"`
	TargetCoreID             string   `json:"targetCoreId"`
	TargetCoreArtifactID     string   `json:"targetCoreArtifactId"`
	TargetDATVersionID       any      `json:"targetDatVersionId"`
	ValidationInputDigest    string   `json:"validationInputDigest"`
	VariantStatus            string   `json:"variantStatus"`
	BlockerCodes             []string `json:"blockerCodes"`
}

//nolint:funlen // Contract branches stay contiguous for a single auditable decision.
func (server *Server) calculateMoveImpact(
	request *http.Request,
	targetID string,
	expected int64,
) (gameMoveImpact, error) {
	var sourceID, sourcePlatform, contentID, contentLogicalName, targetPlatform, targetCore, artifactID string
	var version, targetVersion int64
	var datID sql.NullString
	if err := server.database.QueryRowContext(request.Context(), `
SELECT g.platform_instance_id,
src.platform_id,
g.current_content_revision_id,
COALESCE(content.logical_name,''),
g.version,
target.platform_id,
target.default_core_id,
target.version,
a.id,
(SELECT id
FROM dat_versions
WHERE core_artifact_id=a.id
AND is_active=1)
FROM games g
JOIN platform_instances src ON src.id=g.platform_instance_id
LEFT JOIN game_content_files content ON content.game_content_revision_id=g.current_content_revision_id
AND content.role='CONTENT'
JOIN platform_instances target ON target.id=?
AND target.enabled=1
AND target.deleted_at_ms IS NULL
JOIN core_artifacts a ON a.core_id=target.default_core_id
AND a.enabled=1
WHERE g.id=?
AND g.status='PUBLISHED'
`, targetID, request.PathValue("gameId")).Scan(
		&sourceID,
		&sourcePlatform,
		&contentID,
		&contentLogicalName,
		&version,
		&targetPlatform,
		&targetCore,
		&targetVersion,
		&artifactID,
		&datID,
	); err != nil {
		return gameMoveImpact{}, fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	if version != expected || sourceID == targetID || sourcePlatform != targetPlatform {
		return gameMoveImpact{}, errStaleImpact
	}
	status, code := "NEEDS_VALIDATION", "VARIANT_VALIDATION_REQUIRED"
	biosSnapshot, _, _, err := corevalidation.ResolveBIOS(
		request.Context(),
		server.database,
		artifactID,
		contentLogicalName,
	)
	if err != nil {
		return gameMoveImpact{}, fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	inputDigest, err := corevalidation.ValidationInputDigest(artifactID, contentID, datID, biosSnapshot)
	if err != nil {
		return gameMoveImpact{}, fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	var storedStatus, storedCode string
	err = server.database.QueryRowContext(request.Context(), `
SELECT r.status,
r.compatibility_code
FROM game_variants v
JOIN game_variant_revisions r ON r.id=v.current_revision_id
WHERE v.game_id=?
AND v.core_id=?
AND r.validation_input_digest=?
`, request.PathValue("gameId"), targetCore, inputDigest).
		Scan(&storedStatus, &storedCode)
	switch {
	case err == nil:
		status, code = storedStatus, storedCode
	case errors.Is(err, sql.ErrNoRows):
		err = server.database.QueryRowContext(request.Context(), `
SELECT r.status,
r.compatibility_code
FROM game_variants v
JOIN game_variant_revisions r ON r.game_variant_id=v.id
WHERE v.game_id=?
AND v.core_id=?
AND r.validation_input_digest=?
ORDER BY r.created_at_ms DESC,
r.id DESC LIMIT 1
`, request.PathValue("gameId"), targetCore, inputDigest).
			Scan(&storedStatus, &storedCode)
		if err == nil && storedStatus != "READY" {
			status, code = storedStatus, storedCode
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return gameMoveImpact{}, fmt.Errorf("httpapi/game_handlers: %w", err)
		}
	default:
		return gameMoveImpact{}, fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	blockers := []string{}
	if status != "READY" {
		blockers = append(blockers, code)
	}
	return gameMoveImpact{
		Action:                   "MOVE_GAME",
		GameID:                   request.PathValue("gameId"),
		GameVersion:              version,
		ContentRevisionID:        contentID,
		SourcePlatformInstanceID: sourceID,
		TargetPlatformInstanceID: targetID,
		TargetPlatformVersion:    targetVersion,
		TargetCoreID:             targetCore,
		TargetCoreArtifactID:     artifactID,
		TargetDATVersionID:       nullableString(datID),
		ValidationInputDigest:    inputDigest,
		VariantStatus:            status,
		BlockerCodes:             blockers,
	}, nil
}

func moveDigest(impact gameMoveImpact) string {
	encoded, _ := json.Marshal(impact)
	digest := sha256.Sum256(encoded)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func (server *Server) previewGameMove(writer http.ResponseWriter, request *http.Request) {
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
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
	var body struct {
		TargetPlatformInstanceID string `json:"targetPlatformInstanceId"`
	}
	if err := decodeJSON(writer, request, &body, 4096); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "移动预览无效", map[string]any{})
		return
	}
	impact, err := server.calculateMoveImpact(request, body.TargetPlatformInstanceID, expected)
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"GAME_MOVE_TARGET_INVALID",
			"只能移动到同基础平台的其他目录",
			map[string]any{},
		)
		return
	}
	if impact.VariantStatus == "NEEDS_VALIDATION" {
		pending, ensureErr := server.launcher.EnsureVariantForMove(
			request.Context(),
			request.PathValue("gameId"),
			impact.TargetCoreID,
		)
		if ensureErr != nil {
			writeError(
				writer,
				request,
				http.StatusConflict,
				"VARIANT_VALIDATION_FAILED",
				"目标核心验证无法创建或已失败",
				map[string]any{},
			)
			return
		}
		if pending.Status == "VALIDATION_PENDING" {
			writeJSON(
				writer,
				http.StatusAccepted,
				map[string]any{"status": pending.Status, "jobId": pending.JobID, "retryAfterMs": pending.RetryAfterMS},
			)
			server.resumeMoveValidationAfterIdempotency(context.WithoutCancel(request.Context()), pending.JobID)
			return
		}
		impact, err = server.calculateMoveImpact(request, body.TargetPlatformInstanceID, expected)
		if err != nil || impact.VariantStatus == "NEEDS_VALIDATION" {
			writeError(writer, request, http.StatusConflict, "IMPACT_PREVIEW_STALE", "验证完成后移动输入已变化", map[string]any{})
			return
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"impact": impact, "impactDigest": moveDigest(impact)})
}

func (server *Server) resumeMoveValidationAfterIdempotency(ctx context.Context, jobID string) {
	go func() {
		// Move preview responses are persisted while this mutex is held. Waiting
		// here keeps the queued Job observable to concurrent idempotent requests
		// before a very small validation can become READY.
		server.idempotency.Lock()
		var state string
		err := server.database.QueryRowContext(ctx, `
SELECT state
FROM jobs
WHERE id=?
`, jobID).Scan(&state)
		server.idempotency.Unlock()
		if err == nil && state == "QUEUED" {
			server.launcher.ResumeValidationJob(ctx, jobID)
		}
	}()
}

//nolint:funlen // Contract branches stay contiguous for a single auditable decision.
func (server *Server) moveGame(writer http.ResponseWriter, request *http.Request) {
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
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
	var body struct {
		TargetPlatformInstanceID string `json:"targetPlatformInstanceId"`
		ImpactDigest             string `json:"impactDigest"`
		ConfirmBlocked           bool   `json:"confirmBlocked"`
	}
	if err := decodeJSON(writer, request, &body, 4096); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "移动请求无效", map[string]any{})
		return
	}
	impact, err := server.calculateMoveImpact(request, body.TargetPlatformInstanceID, expected)
	if err != nil || subtle.ConstantTimeCompare([]byte(moveDigest(impact)), []byte(body.ImpactDigest)) != 1 {
		writeError(writer, request, http.StatusConflict, "IMPACT_PREVIEW_STALE", "移动影响已变化", map[string]any{})
		return
	}
	if len(impact.BlockerCodes) > 0 && !body.ConfirmBlocked {
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"MOVE_TARGET_CORE_BLOCKED",
			"目标目录默认核心不可用",
			map[string]any{"blockerCodes": impact.BlockerCodes},
		)
		return
	}
	transaction, err := server.database.BeginTx(request.Context(), nil)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer cleanup.Rollback(transaction)
	now := server.now().UnixMilli()
	result, err := transaction.ExecContext(
		request.Context(),
		`
UPDATE games
SET platform_instance_id=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?
`,
		body.TargetPlatformInstanceID,
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
		"GAME_MOVED",
		"GAME",
		request.PathValue("gameId"),
		map[string]any{"platformInstanceId": impact.SourcePlatformInstanceID},
		map[string]any{
			"platformInstanceId": body.TargetPlatformInstanceID,
			"targetCoreId":       impact.TargetCoreID,
			"variantStatus":      impact.VariantStatus,
		},
		now,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected+1))
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"gameId":             request.PathValue("gameId"),
			"platformInstanceId": body.TargetPlatformInstanceID,
			"version":            expected + 1,
			"updatedAtMs":        now,
		},
	)
}

func (server *Server) scrapeGame(writer http.ResponseWriter, request *http.Request) {
	expected, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct {
		MetadataProvider string `json:"metadataProvider"`
	}
	if decodeJSON(writer, request, &body, 4096) != nil || body.MetadataProvider != "HASHEOUS" {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "游戏只支持显式 Hasheous 重刮削", map[string]any{})
		return
	}
	scheduled, version, err := server.metadata.ScheduleGame(request.Context(), request.PathValue("gameId"), expected)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏内容或版本已经变化", map[string]any{})
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version))
	writeJSON(
		writer,
		http.StatusAccepted,
		map[string]any{"scrapeRunId": scheduled.RunID, "jobId": scheduled.JobID, "state": "QUEUED", "version": version},
	)
}

//nolint:funlen // Cursor validation and the candidate/evidence projection form one stable response contract.
func (server *Server) gameScrapeCandidates(writer http.ResponseWriter, request *http.Request) {
	var runID, contentID string
	err := server.database.QueryRowContext(request.Context(), `
SELECT r.id,
r.game_content_revision_id
FROM metadata_scrape_runs r
JOIN games g ON g.id=r.game_id
AND g.current_content_revision_id=r.game_content_revision_id
WHERE r.game_id=?
AND r.provider='HASHEOUS'
AND r.state='COMPLETED'
ORDER BY r.created_at_ms DESC,
r.id DESC LIMIT 1
`, request.PathValue("gameId")).
		Scan(&runID, &contentID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(
			writer,
			http.StatusOK,
			map[string]any{
				"gameId":            request.PathValue("gameId"),
				"contentRevisionId": nil,
				"scrapeRunId":       nil,
				"items":             []any{},
			},
		)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	rows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT id,
provider_game_id,
normalized_metadata_json,
evidence_json,
created_at_ms,
(SELECT count(*)
FROM scrape_candidate_hits h
WHERE h.scrape_candidate_id=c.id)
FROM scrape_candidates c
WHERE scrape_run_id=?
ORDER BY created_at_ms,
id
`,
		runID,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	type candidateRecord struct {
		id, providerID, metadataJSON, evidenceJSON string
		createdAt, hitCount                        int64
	}
	records := make([]candidateRecord, 0)
	for rows.Next() {
		var record candidateRecord
		if err := rows.Scan(
			&record.id,
			&record.providerID,
			&record.metadataJSON,
			&record.evidenceJSON,
			&record.createdAt,
			&record.hitCount,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := rows.Close(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		var metadata, evidence map[string]any
		_ = json.Unmarshal([]byte(record.metadataJSON), &metadata)
		_ = json.Unmarshal([]byte(record.evidenceJSON), &evidence)
		assets, assetErr := server.reviewCandidateAssets(request, record.id)
		if assetErr != nil {
			server.databaseError(writer, request, assetErr)
			return
		}
		items = append(
			items,
			map[string]any{
				"candidateId":    record.id,
				"providerGameId": record.providerID,
				"metadata":       metadata,
				"evidence":       evidence,
				"assets":         assets,
				"hitCount":       record.hitCount,
				"createdAtMs":    record.createdAt,
			},
		)
	}
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"gameId":            request.PathValue("gameId"),
			"contentRevisionId": contentID,
			"scrapeRunId":       runID,
			"items":             items,
		},
	)
}

type applyCandidateRequest struct {
	Fields         []string                     `json:"fields"`
	SelectedAssets libraryimport.SelectedAssets `json:"selectedAssets"`
}

//nolint:funlen // Candidate freshness checks, revision creation, media linking, and audit write share one transaction.
func (server *Server) applyGameScrapeCandidate(writer http.ResponseWriter, request *http.Request) {
	expected, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body applyCandidateRequest
	if decodeJSON(writer, request, &body, 32<<10) != nil || !validCandidateFields(body.Fields) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "候选采用字段无效", map[string]any{})
		return
	}
	transaction, err := server.database.BeginTx(request.Context(), nil)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer cleanup.Rollback(transaction)
	var currentMetadataID, contentID, candidateMetadataJSON string
	var current gameMetadata
	var version int64
	err = transaction.QueryRowContext(request.Context(), `
SELECT g.current_metadata_revision_id,
g.current_content_revision_id,
g.version,
m.title,
m.description,
m.developer,
m.publisher,
m.genre,
m.players,
m.release_year,
c.normalized_metadata_json
FROM games g
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN scrape_candidates c ON c.id=?
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
AND r.game_id=g.id
AND r.game_content_revision_id=g.current_content_revision_id
AND r.state='COMPLETED'
WHERE g.id=?
AND g.status='PUBLISHED'
AND r.id=(SELECT id
FROM metadata_scrape_runs
WHERE game_id=g.id
AND game_content_revision_id=g.current_content_revision_id
AND provider='HASHEOUS'
AND state='COMPLETED'
ORDER BY created_at_ms DESC,
id DESC LIMIT 1)
`, request.PathValue("candidateId"), request.PathValue("gameId")).
		Scan(
			&currentMetadataID,
			&contentID,
			&version,
			&current.Title,
			&current.Description,
			&current.Developer,
			&current.Publisher,
			&current.Genre,
			&current.Players,
			&current.ReleaseYear,
			&candidateMetadataJSON,
		)
	if err != nil || version != expected {
		writeError(writer, request, http.StatusConflict, "SCRAPE_CANDIDATE_STALE", "候选不是当前内容的最新批次", map[string]any{})
		return
	}
	var candidate map[string]any
	if json.Unmarshal([]byte(candidateMetadataJSON), &candidate) != nil {
		server.databaseError(writer, request, errCandidateMetadata)
		return
	}
	applyMetadataFields(&current, candidate, body.Fields)
	revisionID, assetIDs, err := server.createCandidateMetadataRevision(
		request,
		transaction,
		request.PathValue("gameId"),
		currentMetadataID,
		request.PathValue("candidateId"),
		current,
		body.SelectedAssets,
	)
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"SCRAPE_ASSET_INVALID",
			"候选媒体不可用或归属不匹配",
			map[string]any{},
		)
		return
	}
	now := server.now().UnixMilli()
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
AND current_content_revision_id=?
`,
		revisionID,
		strings.ToLower(
			strings.Join([]string{current.Title, current.Developer, current.Publisher, current.Genre}, " "),
		),
		now,
		request.PathValue("gameId"),
		expected,
		contentID,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected+1))
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"gameId":             request.PathValue("gameId"),
			"metadataRevisionId": revisionID,
			"assetIds":           assetIDs,
			"version":            expected + 1,
			"updatedAtMs":        now,
		},
	)
}

func validCandidateFields(fields []string) bool {
	allowed := map[string]struct{}{
		"title":       {},
		"description": {},
		"developer":   {},
		"publisher":   {},
		"genre":       {},
		"players":     {},
		"releaseYear": {},
	}
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if _, ok := allowed[field]; !ok {
			return false
		}
		if _, duplicate := seen[field]; duplicate {
			return false
		}
		seen[field] = struct{}{}
	}
	return true
}

func applyMetadataFields(target *gameMetadata, candidate map[string]any, fields []string) {
	for _, field := range fields {
		switch field {
		case "title":
			target.Title, _ = candidate[field].(string)
		case "description":
			target.Description, _ = candidate[field].(string)
		case "developer":
			target.Developer, _ = candidate[field].(string)
		case "publisher":
			target.Publisher, _ = candidate[field].(string)
		case "genre":
			target.Genre, _ = candidate[field].(string)
		case "players":
			if candidate[field] == nil {
				target.Players = sql.NullInt64{}
			} else if value, ok := candidate[field].(float64); ok {
				target.Players = sql.NullInt64{Int64: int64(value), Valid: true}
			}
		case "releaseYear":
			if candidate[field] == nil {
				target.ReleaseYear = sql.NullInt64{}
			} else if value, ok := candidate[field].(float64); ok {
				target.ReleaseYear = sql.NullInt64{Int64: int64(value), Valid: true}
			}
		}
	}
}

func (server *Server) createCandidateMetadataRevision(
	request *http.Request,
	transaction *sql.Tx,
	gameID, previousMetadataID, candidateID string,
	metadata gameMetadata,
	selected libraryimport.SelectedAssets,
) (string, []string, error) {
	revisionID, _ := uuid.NewV7()
	now := server.now().UnixMilli()
	_, err := transaction.ExecContext(
		request.Context(),
		`
INSERT INTO game_metadata_revisions(id,
game_id,
title,
description,
developer,
publisher,
genre,
players,
release_year,
source_kind,
source_ref_id,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
'RESCRAPE_APPLY',
?,
?)
`,
		revisionID.String(),
		gameID,
		metadata.Title,
		metadata.Description,
		metadata.Developer,
		metadata.Publisher,
		metadata.Genre,
		nullableInteger(metadata.Players),
		nullableInteger(metadata.ReleaseYear),
		candidateID,
		now,
	)
	if err != nil {
		return "", nil, fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	if err := copyCurrentGameAssets(
		request.Context(), transaction, gameID, previousMetadataID, revisionID.String(), selected, now,
	); err != nil {
		return "", nil, err
	}
	createdIDs, err := createSelectedCandidateAssets(
		request.Context(), transaction, gameID, revisionID.String(), candidateID, selected, now,
	)
	if err != nil {
		return "", nil, err
	}
	return revisionID.String(), createdIDs, nil
}

type currentGameAsset struct {
	blobID, kind, mediaType string
	ordinal, width, height  int64
}

func copyCurrentGameAssets(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, previousMetadataID, revisionID string,
	selected libraryimport.SelectedAssets,
	now int64,
) error {
	replaceCover := selected.CoverCandidateAssetID != nil
	replaceBackground := selected.BackgroundCandidateAssetID != nil
	replaceScreenshots := len(selected.ScreenshotCandidateAssetIDs) > 0
	rows, err := transaction.QueryContext(ctx, `
SELECT blob_id,kind,ordinal,width_px,height_px,media_type
FROM game_assets
WHERE game_id=?
AND metadata_revision_id=?
AND NOT (kind='COVER' AND ?)
AND NOT (kind='BACKGROUND' AND ?)
AND NOT (kind='SCREENSHOT' AND ?)
ORDER BY kind,ordinal
`, gameID, previousMetadataID, replaceCover, replaceBackground, replaceScreenshots)
	if err != nil {
		return fmt.Errorf("httpapi/game_handlers: query current assets: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	currentAssets := make([]currentGameAsset, 0)
	for rows.Next() {
		var asset currentGameAsset
		if err := rows.Scan(
			&asset.blobID,
			&asset.kind,
			&asset.ordinal,
			&asset.width,
			&asset.height,
			&asset.mediaType,
		); err != nil {
			return fmt.Errorf("httpapi/game_handlers: scan current asset: %w", err)
		}
		currentAssets = append(currentAssets, asset)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("httpapi/game_handlers: scan current assets: %w", err)
	}
	for _, asset := range currentAssets {
		assetID, _ := uuid.NewV7()
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_assets(id,
game_id,
metadata_revision_id,
blob_id,
kind,
ordinal,
width_px,
height_px,
media_type,
created_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?)
`, assetID.String(), gameID, revisionID, asset.blobID, asset.kind, asset.ordinal, asset.width, asset.height,
			asset.mediaType, now); err != nil {
			return fmt.Errorf("httpapi/game_handlers: preserve current asset: %w", err)
		}
	}
	return nil
}

type selectedCandidateAsset struct {
	id      string
	kind    string
	ordinal int64
}

func createSelectedCandidateAssets(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, revisionID, candidateID string,
	selected libraryimport.SelectedAssets,
	now int64,
) ([]string, error) {
	selectedAssets := selectedCandidateAssets(selected)
	createdIDs := make([]string, 0, len(selectedAssets))
	for _, selectedAsset := range selectedAssets {
		createdID, err := createSelectedCandidateAsset(
			ctx, transaction, gameID, revisionID, candidateID, selectedAsset, now,
		)
		if err != nil {
			return nil, err
		}
		createdIDs = append(createdIDs, createdID)
	}
	return createdIDs, nil
}

func selectedCandidateAssets(selected libraryimport.SelectedAssets) []selectedCandidateAsset {
	assets := make([]selectedCandidateAsset, 0, len(selected.ScreenshotCandidateAssetIDs)+2)
	if selected.CoverCandidateAssetID != nil {
		assets = append(
			assets,
			selectedCandidateAsset{id: *selected.CoverCandidateAssetID, kind: "COVER", ordinal: 0},
		)
	}
	if selected.BackgroundCandidateAssetID != nil {
		assets = append(
			assets,
			selectedCandidateAsset{id: *selected.BackgroundCandidateAssetID, kind: "BACKGROUND", ordinal: 0},
		)
	}
	for ordinal, selectedID := range selected.ScreenshotCandidateAssetIDs {
		assets = append(
			assets,
			selectedCandidateAsset{id: selectedID, kind: "SCREENSHOT", ordinal: int64(ordinal)},
		)
	}
	return assets
}

func createSelectedCandidateAsset(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, revisionID, candidateID string,
	selected selectedCandidateAsset,
	now int64,
) (string, error) {
	var blobID, kind, mediaType string
	var sourceOrdinal, width, height int64
	err := transaction.QueryRowContext(ctx, `
SELECT blob_id,
kind_hint,
ordinal,
width_px,
height_px,
media_type
FROM scrape_candidate_assets
WHERE id=?
AND scrape_candidate_id=?
AND status='READY'
`, selected.id, candidateID).
		Scan(&blobID, &kind, &sourceOrdinal, &width, &height, &mediaType)
	if err != nil {
		return "", fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	if kind != selected.kind {
		return "", errCandidateAssetKind
	}
	assetID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_assets(id,
game_id,
metadata_revision_id,
blob_id,
kind,
ordinal,
width_px,
height_px,
media_type,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
		assetID.String(),
		gameID,
		revisionID,
		blobID,
		kind,
		selected.ordinal,
		width,
		height,
		mediaType,
		now,
	); err != nil {
		return "", fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	return assetID.String(), nil
}

//nolint:funlen,gocyclo // Asset upload branches are independent media, ownership, and optimistic-lock contract checks.
func (server *Server) createGameAsset(writer http.ResponseWriter, request *http.Request) {
	expected, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct {
		UploadFileID string `json:"uploadFileId"`
		Kind         string `json:"kind"`
		Ordinal      int64  `json:"ordinal"`
	}
	if decodeJSON(writer, request, &body, 8<<10) != nil ||
		body.Kind != "COVER" && body.Kind != "BACKGROUND" && body.Kind != "SCREENSHOT" ||
		body.Ordinal < 0 ||
		body.Ordinal > 31 ||
		body.Kind != "SCREENSHOT" && body.Ordinal != 0 {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "游戏媒体参数无效", map[string]any{})
		return
	}
	var uploadID, blobID, digest string
	if err := server.database.QueryRowContext(request.Context(), `
SELECT f.upload_session_id,
b.id,
b.sha256
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
WHERE f.id=?
AND f.state='COMPLETE'
`, body.UploadFileID).Scan(&uploadID, &blobID, &digest); err != nil {
		writeError(writer, request, http.StatusUnprocessableEntity, "ASSET_UPLOAD_INVALID", "上传文件不可用", map[string]any{})
		return
	}
	file, err := server.blobs.OpenDigest(digest)
	if err != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "媒体字节不可用", map[string]any{})
		return
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	cleanup.Error("close", file.Close())
	if readErr != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "媒体字节不可读", map[string]any{})
		return
	}
	imageData, err := hasheous.ValidateImage(contents, "")
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"ASSET_IMAGE_INVALID",
			"媒体必须是受限 PNG、JPEG 或 WebP",
			map[string]any{},
		)
		return
	}
	transaction, err := server.database.BeginTx(request.Context(), nil)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer cleanup.Rollback(transaction)
	var currentID string
	var metadata gameMetadata
	var version int64
	err = transaction.QueryRowContext(request.Context(), `
SELECT g.current_metadata_revision_id,
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
AND g.status='PUBLISHED'
`, request.PathValue("gameId")).
		Scan(
			&currentID,
			&version,
			&metadata.Title,
			&metadata.Description,
			&metadata.Developer,
			&metadata.Publisher,
			&metadata.Genre,
			&metadata.Players,
			&metadata.ReleaseYear,
		)
	if err != nil || version != expected {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	revisionID, _ := uuid.NewV7()
	now := server.now().UnixMilli()
	if _, err := transaction.ExecContext(request.Context(), `
INSERT INTO game_metadata_revisions(id,
game_id,
title,
description,
developer,
publisher,
genre,
players,
release_year,
source_kind,
source_ref_id,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
'ADMIN_EDIT',
NULL,
?)
`,
		revisionID.String(),
		request.PathValue("gameId"),
		metadata.Title,
		metadata.Description,
		metadata.Developer,
		metadata.Publisher,
		metadata.Genre,
		nullableInteger(metadata.Players),
		nullableInteger(metadata.ReleaseYear),
		now,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := copyAssetsExcept(
		request,
		transaction,
		request.PathValue("gameId"),
		currentID,
		revisionID.String(),
		body.Kind,
		body.Ordinal,
		now,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	assetID, consumptionID := newUUIDString(), newUUIDString()
	if _, err := transaction.ExecContext(request.Context(), `
INSERT INTO game_assets(id,
game_id,
metadata_revision_id,
blob_id,
kind,
ordinal,
width_px,
height_px,
media_type,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
		assetID,
		request.PathValue("gameId"),
		revisionID.String(),
		blobID,
		body.Kind,
		body.Ordinal,
		imageData.Width,
		imageData.Height,
		imageData.MediaType,
		now,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if _, err := transaction.ExecContext(request.Context(), `
INSERT INTO upload_consumptions(id,
upload_session_id,
upload_file_id,
consumer_type,
consumer_id,
created_at_ms) VALUES(?,
?,
?,
'GAME_ASSET',
?,
?)
`, consumptionID, uploadID, body.UploadFileID, assetID, now); err != nil {
		writeError(writer, request, http.StatusConflict, "UPLOAD_ALREADY_CONSUMED", "上传文件已被其他操作占用", map[string]any{})
		return
	}
	result, err := transaction.ExecContext(
		request.Context(),
		`
UPDATE games
SET current_metadata_revision_id=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?
`,
		revisionID.String(),
		now,
		request.PathValue("gameId"),
		expected,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected+1))
	writeJSON(
		writer,
		http.StatusCreated,
		map[string]any{
			"assetId":            assetID,
			"gameId":             request.PathValue("gameId"),
			"metadataRevisionId": revisionID.String(),
			"kind":               body.Kind,
			"ordinal":            body.Ordinal,
			"widthPx":            imageData.Width,
			"heightPx":           imageData.Height,
			"mediaType":          imageData.MediaType,
			"version":            expected + 1,
			"createdAtMs":        now,
		},
	)
}

var (
	errReviewAssetUploadInvalid  = errors.New("review asset upload invalid")
	errReviewAssetCASUnavailable = errors.New("review asset CAS unavailable")
	errReviewAssetImageInvalid   = errors.New("review asset image invalid")
	errReviewAssetVersion        = errors.New("review asset version conflict")
	errReviewAssetConsumed       = errors.New("review asset upload consumed")
)

type reviewAssetSource struct {
	uploadID string
	blobID   string
	image    hasheous.AssetData
}

type reviewAssetRecord struct {
	id          string
	createdAtMS int64
}

func (server *Server) loadReviewAssetSource(ctx context.Context, uploadFileID string) (reviewAssetSource, error) {
	var source reviewAssetSource
	var digest string
	if err := server.database.QueryRowContext(ctx, `
SELECT f.upload_session_id,b.id,b.sha256
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
WHERE f.id=? AND f.state='COMPLETE'
`, uploadFileID).Scan(&source.uploadID, &source.blobID, &digest); err != nil {
		return reviewAssetSource{}, errReviewAssetUploadInvalid
	}
	file, err := server.blobs.OpenDigest(digest)
	if err != nil {
		return reviewAssetSource{}, errReviewAssetCASUnavailable
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	cleanup.Error("close", file.Close())
	if readErr != nil {
		return reviewAssetSource{}, errReviewAssetCASUnavailable
	}
	source.image, err = hasheous.ValidateImage(contents, "")
	if err != nil {
		return reviewAssetSource{}, errReviewAssetImageInvalid
	}
	return source, nil
}

func (server *Server) persistReviewAsset(
	ctx context.Context,
	itemID, uploadFileID, kind string,
	expected int64,
	source reviewAssetSource,
) (reviewAssetRecord, error) {
	transaction, err := server.database.BeginTx(ctx, nil)
	if err != nil {
		return reviewAssetRecord{}, fmt.Errorf("begin review asset transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var currentVersion int64
	if err := transaction.QueryRowContext(ctx, `
SELECT d.version
FROM review_drafts d
JOIN import_items i ON i.id=d.import_item_id
WHERE i.id=? AND i.state='REVIEW_PENDING'
`, itemID).Scan(&currentVersion); err != nil || currentVersion != expected {
		return reviewAssetRecord{}, errReviewAssetVersion
	}
	var record reviewAssetRecord
	var ownerItemID string
	err = transaction.QueryRowContext(ctx, `
SELECT id,import_item_id,created_at_ms
FROM review_uploaded_assets
WHERE upload_file_id=?
`, uploadFileID).Scan(&record.id, &ownerItemID, &record.createdAtMS)
	switch {
	case err == nil:
		if ownerItemID != itemID {
			return reviewAssetRecord{}, errReviewAssetConsumed
		}
	case errors.Is(err, sql.ErrNoRows):
		record.id = newUUIDString()
		record.createdAtMS = server.now().UnixMilli()
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_uploaded_assets(
id,import_item_id,upload_file_id,blob_id,kind,width_px,height_px,media_type,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?)
`, record.id, itemID, uploadFileID, source.blobID, kind, source.image.Width, source.image.Height,
			source.image.MediaType, record.createdAtMS); err != nil {
			return reviewAssetRecord{}, fmt.Errorf("insert review uploaded asset: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_consumptions(
id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms
) VALUES(?,?,?,'REVIEW_ASSET',?,?)
`, newUUIDString(), source.uploadID, uploadFileID, record.id, record.createdAtMS); err != nil {
			return reviewAssetRecord{}, errReviewAssetConsumed
		}
	default:
		return reviewAssetRecord{}, fmt.Errorf("query existing review uploaded asset: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return reviewAssetRecord{}, fmt.Errorf("commit review asset transaction: %w", err)
	}
	return record, nil
}

func (server *Server) createReviewAsset(writer http.ResponseWriter, request *http.Request) {
	expected, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct {
		UploadFileID string `json:"uploadFileId"`
		Kind         string `json:"kind"`
	}
	if decodeJSON(writer, request, &body, 8<<10) != nil || body.Kind != "COVER" {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "审核封面参数无效", map[string]any{})
		return
	}
	source, err := server.loadReviewAssetSource(request.Context(), body.UploadFileID)
	switch {
	case errors.Is(err, errReviewAssetUploadInvalid):
		writeError(writer, request, http.StatusUnprocessableEntity, "ASSET_UPLOAD_INVALID", "上传文件不可用", map[string]any{})
		return
	case errors.Is(err, errReviewAssetCASUnavailable):
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "媒体字节不可用", map[string]any{})
		return
	case errors.Is(err, errReviewAssetImageInvalid):
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"ASSET_IMAGE_INVALID",
			"封面必须是受限 PNG、JPEG 或 WebP",
			map[string]any{},
		)
		return
	case err != nil:
		server.databaseError(writer, request, err)
		return
	}
	record, err := server.persistReviewAsset(
		request.Context(),
		request.PathValue("importItemId"),
		body.UploadFileID,
		body.Kind,
		expected,
		source,
	)
	switch {
	case errors.Is(err, errReviewAssetVersion):
		writeError(writer, request, http.StatusConflict, "REVIEW_VERSION_CONFLICT", "审核条目已发生变化", map[string]any{})
		return
	case errors.Is(err, errReviewAssetConsumed):
		writeError(writer, request, http.StatusConflict, "UPLOAD_ALREADY_CONSUMED", "上传文件已被其他操作占用", map[string]any{})
		return
	case err != nil:
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected))
	writeJSON(writer, http.StatusCreated, map[string]any{
		"assetId": record.id, "kind": body.Kind, "widthPx": source.image.Width, "heightPx": source.image.Height,
		"mediaType": source.image.MediaType, "url": "/api/v1/admin/review-assets/" + record.id,
		"version": expected, "createdAtMs": record.createdAtMS,
	})
}

func copyAssetsExcept(
	request *http.Request,
	transaction *sql.Tx,
	gameID, currentID, revisionID, skipKind string,
	skipOrdinal int64,
	now int64,
) error {
	rows, err := transaction.QueryContext(
		request.Context(),
		`
SELECT blob_id,
kind,
ordinal,
width_px,
height_px,
media_type
FROM game_assets
WHERE game_id=?
AND metadata_revision_id=?
ORDER BY kind,
ordinal
`,
		gameID,
		currentID,
	)
	if err != nil {
		return fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var blobID, kind, mediaType string
		var ordinal, width, height int64
		if err := rows.Scan(&blobID, &kind, &ordinal, &width, &height, &mediaType); err != nil {
			return fmt.Errorf("httpapi/game_handlers: %w", err)
		}
		if kind == skipKind && ordinal == skipOrdinal {
			continue
		}
		if _, err := transaction.ExecContext(request.Context(), `
INSERT INTO game_assets(id,
game_id,
metadata_revision_id,
blob_id,
kind,
ordinal,
width_px,
height_px,
media_type,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?)
`, newUUIDString(), gameID, revisionID, blobID, kind, ordinal, width, height, mediaType, now); err != nil {
			return fmt.Errorf("httpapi/game_handlers: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan game media: %w", err)
	}
	return nil
}

func newUUIDString() string {
	value, _ := uuid.NewV7()
	return value.String()
}

func (server *Server) contentAsset(writer http.ResponseWriter, request *http.Request) {
	var digest, mediaType string
	err := server.database.QueryRowContext(request.Context(), `
SELECT b.sha256,
a.media_type
FROM game_assets a
JOIN blobs b ON b.id=a.blob_id
JOIN games g ON g.id=a.game_id
WHERE a.id=?
AND g.status='PUBLISHED'
`, request.PathValue("assetId")).
		Scan(&digest, &mediaType)
	if err != nil {
		writeError(writer, request, http.StatusNotFound, "ASSET_NOT_FOUND", "媒体不存在", map[string]any{})
		return
	}
	server.serveBlob(writer, request, digest, mediaType, false)
}

func (server *Server) saveStateScreenshot(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	var digest, mediaType string
	err := server.database.QueryRowContext(request.Context(), `
SELECT b.sha256,
b.media_type
FROM save_states s
JOIN blobs b ON b.id=s.screenshot_blob_id
JOIN games g ON g.id=s.game_id
WHERE s.id=?
AND s.profile_id=?
AND s.deleted_at_ms IS NULL
AND g.status='PUBLISHED'
`, request.PathValue("saveStateId"), principal.ProfileID).Scan(&digest, &mediaType)
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusNotFound,
			"SAVE_SCREENSHOT_NOT_FOUND",
			"存档截图不存在",
			map[string]any{},
		)
		return
	}
	server.serveBlob(writer, request, digest, mediaType, true)
}
