package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"retrom/internal/authn"
	"retrom/internal/cursor"
	saveservice "retrom/internal/service/saves"
)

type saveListFilters struct {
	NormalizedQ        string
	GameID             string
	PlatformID         string
	PlatformInstanceID string
	CoreID             string
	Availability       string
	Digest             string
	CursorCreatedAtMS  *int64
	CursorID           string
}

func parseSaveListFilters(values url.Values, principal authn.Principal) (saveListFilters, error) {
	filters := saveListFilters{
		NormalizedQ: strings.ToLower(strings.Join(strings.Fields(values.Get("q")), " ")),
		GameID:      values.Get("gameId"), PlatformID: values.Get("platformId"),
		PlatformInstanceID: values.Get("platformInstanceId"), CoreID: values.Get("coreId"),
		Availability: values.Get("availability"),
	}
	if filters.Availability == "" {
		filters.Availability = "AVAILABLE"
	}
	switch filters.Availability {
	case "AVAILABLE":
	case "BLOCKED":
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
	filters.CursorCreatedAtMS = &createdAt
	filters.CursorID = payload.ID
	return nil
}

func projectSaveListItem(row saveservice.ListItem) map[string]any {
	reasons := []any{}
	switch row.CompatibilityStatus {
	case "INCOMPATIBLE_RUNTIME":
		reasons = append(reasons, map[string]any{"code": "SAVE_RUNTIME_INCOMPATIBLE"})
	case "CORE_UNAVAILABLE":
		reasons = append(reasons, map[string]any{"code": "SAVE_CORE_UNAVAILABLE"})
	}
	available := row.GameStatus == "PUBLISHED" && row.CompatibilityStatus == "AVAILABLE"
	return map[string]any{
		"saveStateId": row.ID, "gameId": row.GameID, "gameTitle": row.GameTitle,
		"name": row.Name, "version": row.Version, "createdAtMs": row.CreatedAtMS,
		"lastSyncedAtMs": saveNullableInteger(row.LastSyncedAtMS),
		"discIndex":      saveNullableInteger(row.DiscIndex), "discLabel": saveDiscLabel(row.DiscIndex),
		"activeDurationMs": row.ActiveDurationMS, "sizeBytes": row.SizeBytes,
		"screenshotUrl": optionalSaveScreenshotURL(row.ID, row.HasScreenshot),
		"core":          map[string]any{"id": row.CoreID, "name": row.CoreName},
		"platformId":    row.PlatformID,
		"platform":      map[string]any{"id": row.PlatformID, "name": row.PlatformName},
		"platformInstance": map[string]any{
			"id": row.InstanceID, "name": row.InstanceName,
		},
		"availability": map[string]any{
			"status":  map[bool]string{true: "AVAILABLE", false: "BLOCKED"}[available],
			"reasons": reasons,
		},
	}
}

func saveNullableInteger(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func saveDiscLabel(value *int64) any {
	if value == nil {
		return nil
	}
	return fmt.Sprintf("光盘 %d", *value+1)
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
	rows, err := server.saveService.List(request.Context(), saveservice.ListQuery{
		ProfileID: principal.ProfileID, Query: filters.NormalizedQ,
		GameID: filters.GameID, PlatformID: filters.PlatformID,
		PlatformInstanceID: filters.PlatformInstanceID, CoreID: filters.CoreID,
		Availability: filters.Availability, CursorCreatedAtMS: filters.CursorCreatedAtMS,
		CursorID: filters.CursorID, Limit: limit + 1,
	})
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, projectSaveListItem(row))
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
		if synced, ok := last["lastSyncedAtMs"].(int64); ok {
			createdAtMS = synced
		}
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
	err = server.saveService.Rename(request.Context(), saveservice.RenameRequest{
		SaveStateID: request.PathValue("saveStateId"), ProfileID: principal.ProfileID,
		Name: body.Name, ExpectedVersion: expected, UpdatedAtMS: now,
	})
	if errors.Is(err, saveservice.ErrNotFound) {
		writeError(writer, request, http.StatusNotFound, "SAVE_STATE_NOT_FOUND", "存档不存在", map[string]any{})
		return
	}
	if errors.Is(err, saveservice.ErrVersionConflict) {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "存档已被修改", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
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
	err = server.saveService.Delete(request.Context(), saveservice.DeleteRequest{
		SaveStateID: request.PathValue("saveStateId"), ProfileID: principal.ProfileID,
		ExpectedVersion: expected, UpdatedAtMS: now,
	})
	if errors.Is(err, saveservice.ErrNotFound) {
		writeError(writer, request, http.StatusNotFound, "SAVE_STATE_NOT_FOUND", "存档不存在", map[string]any{})
		return
	}
	if errors.Is(err, saveservice.ErrVersionConflict) {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "存档已被修改", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected+1))
	writer.WriteHeader(http.StatusNoContent)
}
