package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/cursor"
)

type coreImpactGame struct {
	GameID                  string `json:"gameId"`
	GameVersion             int64  `json:"gameVersion"`
	MetadataRevisionID      string `json:"metadataRevisionId"`
	ContentRevisionID       string `json:"contentRevisionId"`
	TargetVariantRevisionID any    `json:"targetVariantRevisionId"`
	TargetVariantStatus     any    `json:"targetVariantStatus"`
	TargetCompatibilityCode any    `json:"targetCompatibilityCode"`
}

type coreImpact struct {
	Action                  string           `json:"action"`
	PlatformInstanceID      string           `json:"platformInstanceId"`
	PlatformInstanceVersion int64            `json:"platformInstanceVersion"`
	CoreID                  string           `json:"coreId"`
	CoreArtifactID          string           `json:"coreArtifactId"`
	DATVersionID            any              `json:"datVersionId"`
	Games                   []coreImpactGame `json:"games"`
}

func impactDigest(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// Per-game readiness projection and blocker counts must be derived from one consistent query snapshot.
func (server *Server) calculateCoreImpact(
	request *http.Request,
	instanceID, coreID string,
	expected int64,
) (coreImpact, map[string]int64, []map[string]any, error) {
	var platformID string
	var version int64
	err := server.database.QueryRowContext(request.Context(), `
SELECT platform_id,
version
FROM platform_instances
WHERE id=?
AND deleted_at_ms IS NULL
`, instanceID).
		Scan(&platformID, &version)
	if err != nil || version != expected {
		return coreImpact{}, nil, nil, errStaleImpact
	}
	var allowed int
	if err := server.database.QueryRowContext(request.Context(), `
SELECT count(*)
FROM platform_cores
WHERE platform_id=?
AND core_id=?
AND enabled=1
`, platformID, coreID).Scan(&allowed); err != nil ||
		allowed != 1 {
		return coreImpact{}, nil, nil, errInvalidCore
	}
	var artifactID string
	var datVersionID sql.NullString
	if err := server.database.QueryRowContext(request.Context(), `
SELECT a.id,
(SELECT id
FROM dat_versions
WHERE core_artifact_id=a.id
AND is_active=1)
FROM core_artifacts a
WHERE a.core_id=?
AND a.runtime_version=?
AND a.selected_for_new_bindings=1 AND a.available_for_launch=1
`, coreID, server.config.ActiveEJSVersion).Scan(&artifactID, &datVersionID); err != nil {
		return coreImpact{}, nil, nil, errInvalidCore
	}
	rows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT g.id,
g.version,
g.current_metadata_revision_id,
g.current_content_revision_id,
r.id,
r.status,
r.compatibility_code
FROM games g
LEFT JOIN game_variants v ON v.game_id=g.id
AND v.core_id=?
LEFT JOIN game_variant_revisions r ON r.id=v.current_revision_id
AND r.game_content_revision_id=g.current_content_revision_id
WHERE g.platform_instance_id=?
AND g.status='PUBLISHED'
ORDER BY g.id
`,
		coreID,
		instanceID,
	)
	if err != nil {
		return coreImpact{}, nil, nil, fmt.Errorf("httpapi/platform_handlers: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	counts := map[string]int64{"ready": 0, "needsValidation": 0, "blocked": 0}
	items := make([]map[string]any, 0)
	games := make([]coreImpactGame, 0)
	for rows.Next() {
		var game coreImpactGame
		var revisionID, status, code sql.NullString
		if err := rows.Scan(
			&game.GameID,
			&game.GameVersion,
			&game.MetadataRevisionID,
			&game.ContentRevisionID,
			&revisionID,
			&status,
			&code,
		); err != nil {
			return coreImpact{}, nil, nil, fmt.Errorf("httpapi/platform_handlers: %w", err)
		}
		game.TargetVariantRevisionID = nullableString(revisionID)
		game.TargetVariantStatus = nullableString(status)
		game.TargetCompatibilityCode = nullableString(code)
		projected := "NEEDS_VALIDATION"
		switch {
		case status.Valid && status.String == "READY":
			projected = "READY"
			counts["ready"]++
		case status.Valid:
			projected = "BLOCKED"
			counts["blocked"]++
		default:
			counts["needsValidation"]++
		}
		games = append(games, game)
		items = append(
			items,
			map[string]any{"gameId": game.GameID, "status": projected, "blockerCode": nullableString(code)},
		)
	}
	impact := coreImpact{
		Action:                  "CHANGE_DEFAULT_CORE",
		PlatformInstanceID:      instanceID,
		PlatformInstanceVersion: version,
		CoreID:                  coreID,
		CoreArtifactID:          artifactID,
		DATVersionID:            nullableString(datVersionID),
		Games:                   games,
	}
	if err := rows.Err(); err != nil {
		return coreImpact{}, nil, nil, fmt.Errorf("scan platform core impact: %w", err)
	}
	return impact, counts, items, nil
}

func (server *Server) previewDefaultCore(writer http.ResponseWriter, request *http.Request) {
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
		CoreID string  `json:"coreId"`
		Cursor *string `json:"cursor"`
		Limit  int     `json:"limit"`
	}
	if err := decodeJSON(writer, request, &body, 16<<10); err != nil || body.CoreID == "" ||
		body.Limit < 1 || body.Limit > 100 {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "核心预览请求无效", map[string]any{})
		return
	}
	impact, counts, items, err := server.calculateCoreImpact(
		request,
		request.PathValue("platformInstanceId"),
		body.CoreID,
		expected,
	)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "IMPACT_PREVIEW_STALE", "目录或影响输入已变化", map[string]any{})
		return
	}
	digest := impactDigest(impact)
	page, nextCursor, err := server.paginateCoreImpact(
		request.Context(),
		request.PathValue("platformInstanceId"),
		body.CoreID,
		expected,
		digest,
		items,
		body.Cursor,
		body.Limit,
	)
	if errors.Is(err, cursor.ErrInvalid) {
		writeError(writer, request, http.StatusConflict, "IMPACT_PREVIEW_STALE", "目录或影响输入已变化", map[string]any{})
		return
	}
	if err != nil {
		writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "无法生成预览游标", map[string]any{})
		return
	}
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"coreId":                  body.CoreID,
			"platformInstanceVersion": expected,
			"counts":                  counts,
			"items":                   page,
			"nextCursor":              nextCursor,
			"impactDigest":            digest,
		},
	)
}

func (server *Server) paginateCoreImpact(
	ctx context.Context,
	instanceID, coreID string,
	expected int64,
	digest string,
	items []map[string]any,
	cursorToken *string,
	limit int,
) ([]map[string]any, *string, error) {
	principal, _ := authn.PrincipalFromContext(ctx)
	filterDigest := cursor.FilterDigest(
		map[string]any{
			"principalId":             principal.UserID,
			"platformInstanceId":      instanceID,
			"platformInstanceVersion": expected,
			"coreId":                  coreID,
			"impactDigest":            digest,
		},
	)
	start := 0
	if cursorToken != nil {
		payload, decodeErr := server.cursors.Decode(
			*cursorToken,
			"postAdminPlatformDefaultCorePreview",
			filterDigest,
			"GAME_ID_ASC",
		)
		if decodeErr != nil || len(payload.SortValues) != 1 || payload.SortValues[0] != digest {
			return nil, nil, cursor.ErrInvalid
		}
		start = len(items)
		for index, item := range items {
			if item["gameId"] == payload.ID {
				start = index + 1
				break
			}
		}
		if start == len(items) && (len(items) == 0 || items[len(items)-1]["gameId"] != payload.ID) {
			return nil, nil, cursor.ErrInvalid
		}
	}
	end := min(start+limit, len(items))
	page := items[start:end]
	var nextCursor *string
	if end < len(items) {
		lastID, ok := page[len(page)-1]["gameId"].(string)
		if !ok {
			return nil, nil, cursor.ErrInvalid
		}
		token, encodeErr := server.cursors.Encode(
			cursor.Payload{
				OperationID:  "postAdminPlatformDefaultCorePreview",
				FilterDigest: filterDigest,
				SortCode:     "GAME_ID_ASC",
				SortValues:   []string{digest},
				ID:           lastID,
			},
		)
		if encodeErr != nil {
			return nil, nil, fmt.Errorf("encode core impact cursor: %w", encodeErr)
		}
		nextCursor = &token
	}
	return page, nextCursor, nil
}

// Impact digest validation, core switch, revalidation scheduling, and audit write are one transaction.
func (server *Server) changeDefaultCore(writer http.ResponseWriter, request *http.Request) {
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
		CoreID         string `json:"coreId"`
		ImpactDigest   string `json:"impactDigest"`
		ConfirmBlocked bool   `json:"confirmBlocked"`
	}
	if err := decodeJSON(writer, request, &body, 16<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "核心变更请求无效", map[string]any{})
		return
	}
	impact, counts, _, err := server.calculateCoreImpact(
		request,
		request.PathValue("platformInstanceId"),
		body.CoreID,
		expected,
	)
	actualDigest := impactDigest(impact)
	if err != nil || subtle.ConstantTimeCompare([]byte(actualDigest), []byte(body.ImpactDigest)) != 1 {
		writeError(writer, request, http.StatusConflict, "IMPACT_PREVIEW_STALE", "目录或影响输入已变化", map[string]any{})
		return
	}
	if counts["blocked"] > 0 && !body.ConfirmBlocked {
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"DEFAULT_CORE_BLOCKED",
			"部分游戏无法使用目标核心",
			map[string]any{"blockedCount": counts["blocked"]},
		)
		return
	}
	now := server.now().UnixMilli()
	transaction, err := server.database.BeginTx(request.Context(), nil)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer cleanup.Rollback(transaction)
	result, err := transaction.ExecContext(
		request.Context(),
		`
UPDATE platform_instances
SET default_core_id=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?
`,
		body.CoreID,
		now,
		request.PathValue("platformInstanceId"),
		expected,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "平台目录已被修改", map[string]any{})
		return
	}
	if err := insertAudit(
		request,
		transaction,
		"PLATFORM_DEFAULT_CORE_CHANGED",
		"PLATFORM_INSTANCE",
		request.PathValue("platformInstanceId"),
		map[string]any{"version": expected},
		map[string]any{"defaultCoreId": body.CoreID, "version": expected + 1, "impactDigest": body.ImpactDigest},
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
			"id":            request.PathValue("platformInstanceId"),
			"defaultCoreId": body.CoreID,
			"version":       expected + 1,
			"updatedAtMs":   now,
		},
	)
}
