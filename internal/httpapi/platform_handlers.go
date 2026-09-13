package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"retrom/internal/authn"
	"retrom/internal/service/platforminstance"

	"github.com/google/uuid"
)

var errPlatformInstanceOrderInvalid = errors.New("invalid platform instance order")

type createPlatformInstanceRequest struct {
	PlatformID    string `json:"platformId"`
	DefaultCoreID string `json:"defaultCoreId"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	SortOrder     int64  `json:"sortOrder"`
}

type patchPlatformInstanceRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	SortOrder   *int64  `json:"sortOrder,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
}

func validPatchPlatformInstance(body patchPlatformInstanceRequest) bool {
	hasChange := body.Name != nil || body.Description != nil || body.SortOrder != nil || body.Enabled != nil
	return hasChange && (body.Name == nil || validText(*body.Name, 1, 200, false)) &&
		(body.Description == nil || validText(*body.Description, 0, 10_000, true))
}

type reorderPlatformInstanceItem struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

type reorderPlatformInstancesRequest struct {
	Items []reorderPlatformInstanceItem `json:"items"`
}

func validText(value string, minimum, maximum int, allowNewline bool) bool {
	if !utf8.ValidString(value) || value != strings.TrimSpace(value) {
		return false
	}
	count := 0
	for _, character := range value {
		if unicode.IsControl(character) &&
			(!allowNewline || (character != '\n' && character != '\r' && character != '\t')) {
			return false
		}
		count++
	}
	return count >= minimum && count <= maximum
}

func platformSlugBase(name, platformID string) string {
	return platforminstance.SlugBase(name, platformID)
}

func platformSlugWithSuffix(base string, suffix int) string {
	return platforminstance.SlugWithSuffix(base, suffix)
}

func (server *Server) createPlatformInstance(writer http.ResponseWriter, request *http.Request) {
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body createPlatformInstanceRequest
	if err := decodeJSON(writer, request, &body, 32<<10); err != nil || !validText(body.Name, 1, 200, false) ||
		!validText(body.Description, 0, 10_000, true) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "平台目录字段无效", map[string]any{})
		return
	}
	actor := authn.ActorFromContext(request.Context(), "release-setup")
	requestID, _ := request.Context().Value(requestIDKey).(string)
	created, err := server.platformDirectories.Create(request.Context(), platforminstance.AuditActor{
		Kind: actor.Kind, UserID: actor.UserID, Label: actor.Label, RequestID: requestID,
	}, platforminstance.CreateInput{
		PlatformID: body.PlatformID, DefaultCoreID: body.DefaultCoreID,
		Name: body.Name, Description: body.Description, SortOrder: body.SortOrder,
	})
	if errors.Is(err, platforminstance.ErrDefaultCoreInvalid) {
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"PLATFORM_DEFAULT_CORE_INVALID",
			"默认核心不属于该平台",
			map[string]any{},
		)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	item, err := server.readPlatformInstance(request, created.ID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", `"v1"`)
	writeJSON(writer, http.StatusCreated, item)
}

func (server *Server) platformInstanceRecommendations(writer http.ResponseWriter, request *http.Request) {
	result, err := server.platformDirectories.Recommendations(request.Context())
	if errors.Is(err, platforminstance.ErrCatalogInvalid) {
		writeError(
			writer, request, http.StatusInternalServerError, "PLATFORM_CATALOG_INVALID",
			"推荐目录配置无效", map[string]any{},
		)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) applyPlatformInstanceRecommendations(writer http.ResponseWriter, request *http.Request) {
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body map[string]json.RawMessage
	if err := decodeJSON(writer, request, &body, 1024); err != nil {
		return
	}
	if body == nil || len(body) != 0 {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "补全请求必须为空对象", map[string]any{})
		return
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	actor := authn.ActorFromContext(request.Context(), "release-setup")
	requestID, _ := request.Context().Value(requestIDKey).(string)
	response, err := server.platformDirectories.Apply(
		request.Context(),
		platforminstance.AuditActor{
			Kind: actor.Kind, UserID: actor.UserID, Label: actor.Label, RequestID: requestID,
		},
		principal.UserID,
		key,
	)
	switch {
	case errors.Is(err, platforminstance.ErrIdempotencyReused):
		writeError(writer, request, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "幂等键已用于另一请求", map[string]any{})
		return
	case errors.Is(err, platforminstance.ErrCatalogInvalid):
		writeError(
			writer, request, http.StatusInternalServerError, "PLATFORM_CATALOG_INVALID",
			"推荐目录配置无效", map[string]any{},
		)
		return
	case err != nil:
		server.databaseError(writer, request, err)
		return
	}
	for name, value := range response.Headers {
		writer.Header().Set(name, value)
	}
	if response.Replayed {
		writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
	}
	writer.WriteHeader(response.Status)
	_, _ = writer.Write(response.Body)
}

func (server *Server) platformInstance(writer http.ResponseWriter, request *http.Request) {
	item, err := server.readPlatformInstance(request, request.PathValue("platformInstanceId"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(writer, request, http.StatusNotFound, "PLATFORM_INSTANCE_NOT_FOUND", "平台目录不存在", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	version, ok := item["version"].(int64)
	if !ok {
		writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "平台目录投影无效", map[string]any{})
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version))
	writeJSON(writer, http.StatusOK, item)
}

func (server *Server) readPlatformInstance(request *http.Request, id string) (map[string]any, error) {
	instance, err := server.platformDirectories.Read(request.Context(), id, server.config.MultiDiscImportEnabled)
	if errors.Is(err, platforminstance.ErrNotFound) {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("read platform instance: %w", err)
	}
	return map[string]any{
		"id": instance.ID, "platformId": instance.PlatformID, "platformName": instance.PlatformName,
		"defaultCoreId": instance.DefaultCoreID, "defaultCoreName": instance.DefaultCoreName,
		"name": instance.Name, "slug": instance.Slug, "description": instance.Description,
		"sortOrder": instance.SortOrder, "enabled": instance.Enabled, "version": instance.Version,
		"createdAtMs": instance.CreatedAtMS, "updatedAtMs": instance.UpdatedAtMS, "gameCount": instance.GameCount,
		"supportedExtensions": instance.SupportedExtensions, "importCapabilities": instance.ImportCapabilities,
	}, nil
}

func requestedPlatformInstanceOrder(items []reorderPlatformInstanceItem) (map[string]int64, error) {
	requested := make(map[string]int64, len(items))
	for _, item := range items {
		if _, err := uuid.Parse(item.ID); err != nil || item.Version < 1 {
			return nil, errPlatformInstanceOrderInvalid
		}
		if _, exists := requested[item.ID]; exists {
			return nil, errPlatformInstanceOrderInvalid
		}
		requested[item.ID] = item.Version
	}
	return requested, nil
}

func (server *Server) reorderPlatformInstances(writer http.ResponseWriter, request *http.Request) {
	var body reorderPlatformInstancesRequest
	if err := decodeJSON(writer, request, &body, 32<<10); err != nil || len(body.Items) == 0 || len(body.Items) > 100 {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "目录排序数据无效", map[string]any{})
		return
	}
	_, err := requestedPlatformInstanceOrder(body.Items)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "目录排序数据无效", map[string]any{})
		return
	}
	actor := authn.ActorFromContext(request.Context(), "release-setup")
	requestID, _ := request.Context().Value(requestIDKey).(string)
	items := make([]platforminstance.PlatformInstanceOrderItem, 0, len(body.Items))
	for _, item := range body.Items {
		items = append(items, platforminstance.PlatformInstanceOrderItem{ID: item.ID, Version: item.Version})
	}
	result, err := server.platformDirectories.Reorder(request.Context(), platforminstance.AuditActor{
		Kind: actor.Kind, UserID: actor.UserID, Label: actor.Label,
		RequestID: requestID,
	}, items)
	if errors.Is(err, platforminstance.ErrOrderStale) {
		writeError(writer, request, http.StatusConflict, "PLATFORM_INSTANCE_ORDER_STALE", "目录列表已变化，请刷新后重试", map[string]any{})
		return
	}
	if errors.Is(err, platforminstance.ErrVersionConflict) {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "目录已被修改，请刷新后重试", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	resultItems := make([]map[string]any, 0, len(result))
	for _, item := range result {
		resultItems = append(resultItems, map[string]any{
			"id": item.ID, "sortOrder": item.SortOrder, "version": item.Version, "updatedAtMs": item.UpdatedAtMS,
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": resultItems})
}

// Contract branches stay contiguous for a single auditable decision.
func (server *Server) patchPlatformInstance(writer http.ResponseWriter, request *http.Request) {
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
	var body patchPlatformInstanceRequest
	if err := decodeJSON(writer, request, &body, 32<<10); err != nil || !validPatchPlatformInstance(body) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "平台目录字段无效", map[string]any{})
		return
	}
	actor := authn.ActorFromContext(request.Context(), "release-setup")
	requestID, _ := request.Context().Value(requestIDKey).(string)
	result, err := server.platformDirectories.Patch(request.Context(), platforminstance.PlatformInstancePatch{
		ID: request.PathValue("platformInstanceId"), ExpectedVersion: expected,
		Name: body.Name, Description: body.Description, SortOrder: body.SortOrder, Enabled: body.Enabled,
		Actor: platforminstance.AuditActor{Kind: actor.Kind, UserID: actor.UserID, Label: actor.Label, RequestID: requestID},
	})
	if errors.Is(err, platforminstance.ErrNotFound) {
		writeError(writer, request, http.StatusNotFound, "PLATFORM_INSTANCE_NOT_FOUND", "平台目录不存在", map[string]any{})
		return
	}
	if errors.Is(err, platforminstance.ErrVersionConflict) {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "平台目录已被修改", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writeJSON(writer, http.StatusOK, map[string]any{
		"id": result.ID, "name": result.Name, "description": result.Description,
		"sortOrder": result.SortOrder, "enabled": result.Enabled, "version": result.Version,
		"updatedAtMs": result.UpdatedAtMS,
	})
}

// Reference checks, optimistic locking, deletion, and audit write share one transaction.
func (server *Server) deletePlatformInstance(writer http.ResponseWriter, request *http.Request) {
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
	actor := authn.ActorFromContext(request.Context(), "release-setup")
	requestID, _ := request.Context().Value(requestIDKey).(string)
	err = server.platformDirectories.Delete(request.Context(), platforminstance.PlatformInstanceDelete{
		ID: request.PathValue("platformInstanceId"), ExpectedVersion: expected,
		Actor: platforminstance.AuditActor{Kind: actor.Kind, UserID: actor.UserID, Label: actor.Label, RequestID: requestID},
	})
	if errors.Is(err, platforminstance.ErrNotFound) {
		writeError(writer, request, http.StatusNotFound, "PLATFORM_INSTANCE_NOT_FOUND", "平台目录不存在", map[string]any{})
		return
	}
	if errors.Is(err, platforminstance.ErrVersionConflict) {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "平台目录已被修改", map[string]any{})
		return
	}
	if errors.Is(err, platforminstance.ErrNotEmpty) {
		writeError(writer, request, http.StatusConflict, "PLATFORM_INSTANCE_NOT_EMPTY", "非空目录不能删除", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}
