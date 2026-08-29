package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/cursor"
)

type saveListFilters struct {
	Conditions   []string
	Arguments    []any
	NormalizedQ  string
	Availability string
	Digest       string
}

func parseSaveListFilters(values url.Values, principal authn.Principal) (saveListFilters, error) {
	filters := saveListFilters{
		Conditions:   []string{"s.profile_id=?", "s.deleted_at_ms IS NULL", "pi.enabled=1"},
		Arguments:    []any{principal.ProfileID},
		NormalizedQ:  strings.ToLower(strings.Join(strings.Fields(values.Get("q")), " ")),
		Availability: values.Get("availability"),
	}
	if filters.NormalizedQ != "" {
		filters.Conditions = append(filters.Conditions, "(instr(g.search_text,?)>0 OR instr(lower(s.name),?)>0)")
		filters.Arguments = append(filters.Arguments, filters.NormalizedQ, filters.NormalizedQ)
	}
	for _, filter := range []struct{ queryName, column string }{
		{"gameId", "s.game_id"},
		{"platformId", "pi.platform_id"},
		{"platformInstanceId", "pi.id"},
		{"coreId", "a.core_id"},
	} {
		if value := values.Get(filter.queryName); value != "" {
			filters.Conditions = append(filters.Conditions, filter.column+"=?")
			filters.Arguments = append(filters.Arguments, value)
		}
	}
	if filters.Availability == "" {
		filters.Availability = "AVAILABLE"
	}
	switch filters.Availability {
	case "AVAILABLE":
		filters.Conditions = append(filters.Conditions, "g.status='PUBLISHED'", "runtime_compatibility.status='AVAILABLE'")
	case "BLOCKED":
		filters.Conditions = append(
			filters.Conditions,
			"(g.status!='PUBLISHED' OR runtime_compatibility.status!='AVAILABLE')",
		)
	case "ALL":
	default:
		return saveListFilters{}, fmt.Errorf("%w: availability", errUnknownQuery)
	}
	filters.Digest = cursor.FilterDigest(map[string]any{
		"principalId":        principal.UserID,
		"q":                  filters.NormalizedQ,
		"gameId":             values.Get("gameId"),
		"platformId":         values.Get("platformId"),
		"platformInstanceId": values.Get("platformInstanceId"),
		"coreId":             values.Get("coreId"),
		"availability":       filters.Availability,
	})
	return filters, nil
}

func (server *Server) applySaveCursor(values url.Values, filters *saveListFilters) error {
	token := values.Get("cursor")
	if token == "" {
		return nil
	}
	payload, err := server.cursors.Decode(token, "getSaves", filters.Digest, "CREATED_DESC")
	if err != nil || len(payload.SortValues) != 1 {
		return errInvalidCursorPayload
	}
	createdAt, err := strconv.ParseInt(payload.SortValues[0], 10, 64)
	if err != nil {
		return errInvalidCursorPayload
	}
	filters.Conditions = append(filters.Conditions, "(s.created_at_ms<? OR (s.created_at_ms=? AND s.id<?))")
	filters.Arguments = append(filters.Arguments, createdAt, createdAt, payload.ID)
	return nil
}

type saveListRow struct {
	id, gameID, gameTitle, name, coreID, coreName, gameStatus string
	platformID, platformName, instanceID, instanceName        string
	compatibilityStatus                                       string
	version, createdAtMS, activeDurationMS                    int64
	hasScreenshot                                             bool
	discIndex                                                 sql.NullInt64
}

func scanSaveListRows(rows *sql.Rows, capacity int) ([]map[string]any, error) {
	items := make([]map[string]any, 0, capacity)
	for rows.Next() {
		var row saveListRow
		if err := rows.Scan(
			&row.id, &row.gameID, &row.gameTitle, &row.name, &row.version,
			&row.createdAtMS, &row.activeDurationMS, &row.coreID, &row.coreName,
			&row.gameStatus, &row.platformID, &row.platformName, &row.instanceID,
			&row.instanceName, &row.discIndex, &row.hasScreenshot, &row.compatibilityStatus,
		); err != nil {
			return nil, fmt.Errorf("scan save list row: %w", err)
		}
		items = append(items, row.projection())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate save list rows: %w", err)
	}
	return items, nil
}

func (row saveListRow) projection() map[string]any {
	reasons := []any{}
	switch row.compatibilityStatus {
	case "INCOMPATIBLE_RUNTIME":
		reasons = append(reasons, map[string]any{"code": "SAVE_RUNTIME_INCOMPATIBLE"})
	case "CORE_UNAVAILABLE":
		reasons = append(reasons, map[string]any{"code": "SAVE_CORE_UNAVAILABLE"})
	}
	available := row.gameStatus == "PUBLISHED" && row.compatibilityStatus == "AVAILABLE"
	return map[string]any{
		"saveStateId": row.id, "gameId": row.gameID, "gameTitle": row.gameTitle,
		"name": row.name, "version": row.version, "createdAtMs": row.createdAtMS,
		"discIndex": nullableInteger(row.discIndex), "discLabel": discLabel(row.discIndex),
		"activeDurationMs": row.activeDurationMS,
		"screenshotUrl":    optionalSaveScreenshotURL(row.id, row.hasScreenshot),
		"core":             map[string]any{"id": row.coreID, "name": row.coreName},
		"platformId":       row.platformID,
		"platform":         map[string]any{"id": row.platformID, "name": row.platformName},
		"platformInstance": map[string]any{
			"id": row.instanceID, "name": row.instanceName,
		},
		"availability": map[string]any{
			"status":  map[bool]string{true: "AVAILABLE", false: "BLOCKED"}[available],
			"reasons": reasons,
		},
	}
}

// Query projection stays contiguous with pagination assembly.
func (server *Server) saves(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	values := request.URL.Query()
	filters, err := parseSaveListFilters(values, principal)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "存档可用性筛选无效", map[string]any{})
		return
	}
	if err := server.applySaveCursor(values, &filters); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
		return
	}
	limit := 50
	if raw := values.Get("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	query := queryWithConditions(
		`
SELECT s.id,
s.game_id,
m.title,
s.name,
s.version,
s.created_at_ms,
s.active_duration_ms,
a.core_id,
c.name,
g.status,
p.id,
p.name,
pi.id,
pi.name,
s.disc_index,
s.screenshot_blob_id IS NOT NULL,
runtime_compatibility.status
FROM save_states s
JOIN save_state_runtime_compatibility runtime_compatibility
  ON runtime_compatibility.save_state_id=s.id
JOIN games g ON g.id=s.game_id
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN core_artifacts a ON a.id=s.core_artifact_id
JOIN cores c ON c.id=a.core_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
`,
		filters.Conditions,
		` ORDER BY s.created_at_ms DESC,s.id DESC LIMIT ?`,
	)
	filters.Arguments = append(filters.Arguments, limit+1)
	rows, err := server.database.QueryContext(request.Context(), query, filters.Arguments...)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items, err := scanSaveListRows(rows, limit+1)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := projectMapTags(request.Context(), items, "gameId", server.tagService.References); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var nextCursor any
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		createdAtMS, createdOK := last["createdAtMs"].(int64)
		lastID, idOK := last["saveStateId"].(string)
		if !createdOK || !idOK {
			writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "存档分页投影无效", map[string]any{})
			return
		}
		token, err := server.cursors.Encode(
			cursor.Payload{
				OperationID:  "getSaves",
				FilterDigest: filters.Digest,
				SortCode:     "CREATED_DESC",
				SortValues:   []string{strconv.FormatInt(createdAtMS, 10)},
				ID:           lastID,
			},
		)
		if err != nil {
			server.databaseError(writer, request, err)
			return
		}
		nextCursor = token
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"generatedAtMs": server.now().UnixMilli(), "items": items, "nextCursor": nextCursor,
	})
}

func optionalSaveScreenshotURL(saveStateID string, available bool) any {
	if !available {
		return nil
	}
	return saveStateScreenshotURL(saveStateID)
}

func (server *Server) patchSave(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(writer, request, &body, 4096); err != nil || strings.TrimSpace(body.Name) != body.Name ||
		body.Name == "" ||
		len([]rune(body.Name)) > 120 {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "存档名称无效", map[string]any{})
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
	now := server.now().UnixMilli()
	result, err := server.database.ExecContext(
		request.Context(),
		`
UPDATE save_states
SET name=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND profile_id=?
AND version=?
AND deleted_at_ms IS NULL
`,
		body.Name,
		now,
		request.PathValue("saveStateId"),
		principal.ProfileID,
		expected,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		var exists int
		lookupErr := server.database.QueryRowContext(request.Context(), `
SELECT 1 FROM save_states WHERE id=? AND profile_id=? AND deleted_at_ms IS NULL
`, request.PathValue("saveStateId"), principal.ProfileID).Scan(&exists)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			writeError(writer, request, http.StatusNotFound, "SAVE_STATE_NOT_FOUND", "存档不存在", map[string]any{})
			return
		}
		if lookupErr != nil {
			server.databaseError(writer, request, lookupErr)
			return
		}
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "存档已被修改", map[string]any{})
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected+1))
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"saveStateId": request.PathValue("saveStateId"),
			"name":        body.Name,
			"version":     expected + 1,
			"updatedAtMs": now,
		},
	)
}

func (server *Server) deleteSave(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
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
	now := server.now().UnixMilli()
	result, err := server.database.ExecContext(
		request.Context(),
		`
UPDATE save_states
SET deleted_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND profile_id=?
AND version=?
AND deleted_at_ms IS NULL
`,
		now,
		now,
		request.PathValue("saveStateId"),
		principal.ProfileID,
		expected,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		var exists int
		lookupErr := server.database.QueryRowContext(request.Context(), `
SELECT 1 FROM save_states WHERE id=? AND profile_id=? AND deleted_at_ms IS NULL
`, request.PathValue("saveStateId"), principal.ProfileID).Scan(&exists)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			writeError(writer, request, http.StatusNotFound, "SAVE_STATE_NOT_FOUND", "存档不存在", map[string]any{})
			return
		}
		if lookupErr != nil {
			server.databaseError(writer, request, lookupErr)
			return
		}
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "存档已被修改", map[string]any{})
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected+1))
	writer.WriteHeader(http.StatusNoContent)
}
