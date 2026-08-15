package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"retrom/internal/authn"
	"retrom/internal/cursor"
	"retrom/internal/tagging"
)

func writeTagError(writer http.ResponseWriter, request *http.Request, err error) {
	var invalidReferences *tagging.InvalidReferencesError
	switch {
	case errors.As(err, &invalidReferences):
		writeError(writer, request, http.StatusUnprocessableEntity, "TAG_REFERENCE_INVALID", "标签引用无效",
			map[string]any{"invalidTagIds": invalidReferences.IDs})
	case errors.Is(err, tagging.ErrNameInvalid):
		writeError(writer, request, http.StatusUnprocessableEntity, "TAG_NAME_INVALID", "标签名称无效", map[string]any{})
	case errors.Is(err, tagging.ErrNotFound):
		writeError(writer, request, http.StatusNotFound, "TAG_NOT_FOUND", "标签不存在", map[string]any{})
	case errors.Is(err, tagging.ErrGameNotFound):
		writeError(writer, request, http.StatusNotFound, "GAME_NOT_FOUND", "游戏不存在", map[string]any{})
	case errors.Is(err, tagging.ErrNameConflict):
		writeError(writer, request, http.StatusConflict, "TAG_NAME_CONFLICT", "已存在同名活动标签", map[string]any{})
	case errors.Is(err, tagging.ErrLimitReached):
		writeError(writer, request, http.StatusConflict, "TAG_LIMIT_REACHED", "活动标签数量已达上限", map[string]any{})
	case errors.Is(err, tagging.ErrAlreadyDeleted):
		writeError(writer, request, http.StatusConflict, "TAG_ALREADY_DELETED", "标签已经删除", map[string]any{})
	case errors.Is(err, tagging.ErrVersionConflict):
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "资源版本已变化", map[string]any{})
	case errors.Is(err, tagging.ErrAssignmentLimitExceeded):
		writeError(
			writer, request, http.StatusUnprocessableEntity,
			"TAG_ASSIGNMENT_LIMIT_EXCEEDED", "标签数量超过上限", map[string]any{},
		)
	case errors.Is(err, tagging.ErrDeleteConfirmation):
		writeError(
			writer, request, http.StatusUnprocessableEntity,
			"TAG_DELETE_CONFIRMATION_MISMATCH", "标签名称确认不匹配", map[string]any{},
		)
	case errors.Is(err, tagging.ErrReferenceInvalid):
		writeError(writer, request, http.StatusUnprocessableEntity, "TAG_REFERENCE_INVALID", "标签引用无效", map[string]any{})
	case errors.Is(err, tagging.ErrInvalid):
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "标签请求无效", map[string]any{})
	default:
		serverError(writer, request, err)
	}
}

func requireTagIdempotency(writer http.ResponseWriter, request *http.Request) bool {
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return false
	}
	return true
}

func tagActorID(request *http.Request) string {
	principal, _ := authn.PrincipalFromContext(request.Context())
	return principal.UserID
}

func normalizedTagQuery(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func (server *Server) tagPageCursor(
	items []tagging.AdminItem,
	limit int,
	filterDigest, sortCode string,
) ([]tagging.AdminItem, any, error) {
	if len(items) <= limit {
		return items, nil, nil
	}
	last := items[limit-1]
	sortValues := []string{strconv.FormatInt(last.UpdatedAtMS, 10)}
	if sortCode == tagging.SortNameAsc {
		_, nameKey, _, err := tagging.NormalizeName(last.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("normalize tag cursor name: %w", err)
		}
		sortValues = []string{nameKey}
	}
	token, err := server.cursors.Encode(cursor.Payload{
		OperationID: "getAdminTags", FilterDigest: filterDigest, SortCode: sortCode,
		SortValues: sortValues, ID: last.TagID,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("encode tag page cursor: %w", err)
	}
	return items[:limit], token, nil
}

func (server *Server) adminTags(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	status := values.Get("status")
	if status == "" {
		status = tagging.StatusActive
	}
	sortCode := values.Get("sort")
	if sortCode == "" {
		sortCode = tagging.SortNameAsc
	}
	limit := tagging.DefaultListLimit
	if values.Get("limit") != "" {
		limit, _ = strconv.Atoi(values.Get("limit"))
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	query := normalizedTagQuery(values.Get("q"))
	filterDigest := cursor.FilterDigest(map[string]any{
		"principalId": principal.UserID, "q": query, "status": status, "sort": sortCode,
	})
	filter := tagging.ListFilter{Query: query, Status: status, Sort: sortCode, Limit: limit + 1}
	if token := values.Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(token, "getAdminTags", filterDigest, sortCode)
		if err != nil {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "标签分页游标无效", map[string]any{})
			return
		}
		filter.AfterValues, filter.AfterID = payload.SortValues, payload.ID
	}
	items, err := server.tagService.List(request.Context(), filter)
	if err != nil {
		writeTagError(writer, request, err)
		return
	}
	summary, err := server.tagService.Summary(request.Context())
	if err != nil {
		writeTagError(writer, request, err)
		return
	}
	items, nextCursor, err := server.tagPageCursor(items, limit, filterDigest, sortCode)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusOK, map[string]any{
		"generatedAtMs": server.now().UnixMilli(), "summary": summary, "items": items, "nextCursor": nextCursor,
	})
}

func (server *Server) createAdminTag(writer http.ResponseWriter, request *http.Request) {
	if !requireTagIdempotency(writer, request) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(writer, request, &body, 4096); err != nil {
		return
	}
	result, err := server.tagService.Create(request.Context(), tagActorID(request), body.Name)
	if err != nil {
		writeTagError(writer, request, err)
		return
	}
	writer.Header().Set("Location", "/api/v1/admin/tags/"+result.TagID)
	writeTagItem(writer, http.StatusCreated, result)
}

func writeTagItem(writer http.ResponseWriter, status int, result tagging.AdminItem) {
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, status, result)
}

func (server *Server) adminTag(writer http.ResponseWriter, request *http.Request) {
	result, err := server.tagService.Get(request.Context(), request.PathValue("tagId"))
	if err != nil {
		writeTagError(writer, request, err)
		return
	}
	writeTagItem(writer, http.StatusOK, result)
}

func (server *Server) patchAdminTag(writer http.ResponseWriter, request *http.Request) {
	if !requireTagIdempotency(writer, request) {
		return
	}
	expected, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(writer, request, http.StatusPreconditionRequired, "PRECONDITION_REQUIRED", "需要当前标签版本", map[string]any{})
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(writer, request, &body, 4096); err != nil {
		return
	}
	result, err := server.tagService.Rename(
		request.Context(), tagActorID(request), request.PathValue("tagId"), body.Name, expected,
	)
	if err != nil {
		writeTagError(writer, request, err)
		return
	}
	writeTagItem(writer, http.StatusOK, result)
}

func (server *Server) deleteAdminTag(writer http.ResponseWriter, request *http.Request) {
	if !requireTagIdempotency(writer, request) {
		return
	}
	expected, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(writer, request, http.StatusPreconditionRequired, "PRECONDITION_REQUIRED", "需要当前标签版本", map[string]any{})
		return
	}
	var body struct {
		ConfirmName string `json:"confirmName"`
	}
	if err := decodeJSON(writer, request, &body, 4096); err != nil {
		return
	}
	result, _, err := server.tagService.Delete(
		request.Context(), tagActorID(request), request.PathValue("tagId"), body.ConfirmName, expected,
	)
	if err != nil {
		writeTagError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) putAdminGameTags(writer http.ResponseWriter, request *http.Request) {
	if !requireTagIdempotency(writer, request) {
		return
	}
	expected, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(writer, request, http.StatusPreconditionRequired, "PRECONDITION_REQUIRED", "需要当前游戏版本", map[string]any{})
		return
	}
	var body struct {
		TagIDs []string `json:"tagIds"`
	}
	if err := decodeJSON(writer, request, &body, 16384); err != nil {
		return
	}
	if body.TagIDs == nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "tagIds 必填", map[string]any{})
		return
	}
	result, err := server.tagService.ReplaceGameTags(
		request.Context(), tagActorID(request), request.PathValue("gameId"), expected, body.TagIDs,
	)
	if err != nil {
		writeTagError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusOK, result)
}
