package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/contentprofile"
	"retrom/internal/platforminstance"

	"github.com/google/uuid"
)

var (
	errPlatformInstanceOrderInvalid = errors.New("invalid platform instance order")
	errPlatformInstanceOrderVersion = errors.New("platform instance order version conflict")
)

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

type patchPlatformInstanceValues struct {
	name, description string
	sortOrder         int64
	enabled           bool
	version           int64
}

func validPatchPlatformInstance(body patchPlatformInstanceRequest) bool {
	hasChange := body.Name != nil || body.Description != nil || body.SortOrder != nil || body.Enabled != nil
	return hasChange && (body.Name == nil || validText(*body.Name, 1, 200, false)) &&
		(body.Description == nil || validText(*body.Description, 0, 10_000, true))
}

func patchPlatformValues(current map[string]any) (patchPlatformInstanceValues, bool) {
	values := patchPlatformInstanceValues{}
	var ok bool
	values.version, ok = current["version"].(int64)
	if !ok {
		return patchPlatformInstanceValues{}, false
	}
	values.name, ok = current["name"].(string)
	if !ok {
		return patchPlatformInstanceValues{}, false
	}
	values.description, ok = current["description"].(string)
	if !ok {
		return patchPlatformInstanceValues{}, false
	}
	values.sortOrder, ok = current["sortOrder"].(int64)
	if !ok {
		return patchPlatformInstanceValues{}, false
	}
	values.enabled, ok = current["enabled"].(bool)
	return values, ok
}

func (values *patchPlatformInstanceValues) apply(body patchPlatformInstanceRequest) {
	if body.Name != nil {
		values.name = *body.Name
	}
	if body.Description != nil {
		values.description = *body.Description
	}
	if body.SortOrder != nil {
		values.sortOrder = *body.SortOrder
	}
	if body.Enabled != nil {
		values.enabled = *body.Enabled
	}
}

type reorderPlatformInstanceItem struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

type reorderPlatformInstancesRequest struct {
	Items []reorderPlatformInstanceItem `json:"items"`
}

type platformInstanceOrderState struct {
	Version   int64
	SortOrder int64
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

func insertAudit(
	request *http.Request,
	transaction *sql.Tx,
	action, resourceType, resourceID string,
	before, after any,
	now int64,
) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("httpapi/platform_handlers: %w", err)
	}
	var beforeJSON, afterJSON any
	if before != nil {
		value, _ := json.Marshal(before)
		beforeJSON = string(value)
	}
	if after != nil {
		value, _ := json.Marshal(after)
		afterJSON = string(value)
	}
	actor := authn.ActorFromContext(request.Context(), "release-setup")
	_, err = transaction.ExecContext(
		request.Context(),
		`
INSERT INTO audit_events(id,
actor_kind,
actor_user_id,
actor_label,
action,
resource_type,
resource_id,
before_json,
after_json,
diff_json,
request_id,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
'{}',
?,
?)
`,
		id.String(),
		actor.Kind,
		actor.UserID,
		actor.Label,
		action,
		resourceType,
		resourceID,
		beforeJSON,
		afterJSON,
		request.Context().Value(requestIDKey),
		now,
	)
	if err != nil {
		return fmt.Errorf("insert platform audit event: %w", err)
	}
	return nil
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
	var platformID, platformName, defaultCoreID, defaultCoreName, name, slug, description, compatibility string
	var sortOrder, enabled, version, createdAtMS, updatedAtMS, gameCount int64
	err := server.database.QueryRowContext(request.Context(), `
SELECT pi.platform_id,
p.name,
pi.default_core_id,
c.name,
pi.name,
pi.slug,
pi.description,
pi.sort_order,
pi.enabled,
pi.version,
pi.created_at_ms,
pi.updated_at_ms,
(SELECT count(*) FROM games g WHERE g.platform_instance_id=pi.id)
,
COALESCE((SELECT a.compatibility_config_json
 FROM core_artifacts a
 WHERE a.core_id=pi.default_core_id
 AND a.enabled=1
 LIMIT 1),'{}')
FROM platform_instances pi
JOIN platforms p ON p.id=pi.platform_id
JOIN cores c ON c.id=pi.default_core_id
WHERE pi.id=?
AND pi.deleted_at_ms IS NULL
`, id).
		Scan(
			&platformID,
			&platformName,
			&defaultCoreID,
			&defaultCoreName,
			&name,
			&slug,
			&description,
			&sortOrder,
			&enabled,
			&version,
			&createdAtMS,
			&updatedAtMS,
			&gameCount,
			&compatibility,
		)
	if err != nil {
		return nil, fmt.Errorf("httpapi/platform_handlers: %w", err)
	}
	return map[string]any{
		"id":                  id,
		"platformId":          platformID,
		"platformName":        platformName,
		"defaultCoreId":       defaultCoreID,
		"defaultCoreName":     defaultCoreName,
		"name":                name,
		"slug":                slug,
		"description":         description,
		"sortOrder":           sortOrder,
		"enabled":             enabled == 1,
		"version":             version,
		"createdAtMs":         createdAtMS,
		"updatedAtMs":         updatedAtMS,
		"gameCount":           gameCount,
		"supportedExtensions": contentprofile.SupportedExtensions(platformID),
		"importCapabilities": contentcapability.Resolve(
			platformID, enabled == 1, server.config.MultiDiscImportEnabled, compatibility,
		),
	}, nil
}

func readPlatformInstanceOrder(
	ctx context.Context,
	transaction *sql.Tx,
	capacity int,
) (map[string]platformInstanceOrderState, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT id,version,sort_order
FROM platform_instances
WHERE deleted_at_ms IS NULL
ORDER BY sort_order,id
`)
	if err != nil {
		return nil, fmt.Errorf("httpapi/platform order query: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	current := make(map[string]platformInstanceOrderState, capacity)
	for rows.Next() {
		var id string
		var state platformInstanceOrderState
		if err := rows.Scan(&id, &state.Version, &state.SortOrder); err != nil {
			return nil, fmt.Errorf("httpapi/platform order scan: %w", err)
		}
		current[id] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("httpapi/platform order rows: %w", err)
	}
	return current, nil
}

func applyPlatformInstanceOrder(
	request *http.Request,
	transaction *sql.Tx,
	items []reorderPlatformInstanceItem,
	current map[string]platformInstanceOrderState,
	now int64,
) ([]map[string]any, error) {
	resultItems := make([]map[string]any, 0, len(items))
	for index, item := range items {
		sortOrder := int64(index+1) * 100
		result, err := transaction.ExecContext(request.Context(), `
UPDATE platform_instances
SET sort_order=?,version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND deleted_at_ms IS NULL
`, sortOrder, now, item.ID, item.Version)
		if err != nil {
			return nil, fmt.Errorf("httpapi/platform order update: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("httpapi/platform order affected rows: %w", err)
		}
		if changed != 1 {
			return nil, errPlatformInstanceOrderVersion
		}
		previous := current[item.ID]
		before := map[string]any{"version": previous.Version, "sortOrder": previous.SortOrder}
		after := map[string]any{"version": item.Version + 1, "sortOrder": sortOrder}
		if err := insertAudit(
			request,
			transaction,
			"PLATFORM_INSTANCE_REORDERED",
			"PLATFORM_INSTANCE",
			item.ID,
			before,
			after,
			now,
		); err != nil {
			return nil, fmt.Errorf("httpapi/platform order audit: %w", err)
		}
		resultItems = append(resultItems, map[string]any{
			"id": item.ID, "sortOrder": sortOrder, "version": item.Version + 1, "updatedAtMs": now,
		})
	}
	return resultItems, nil
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
	requested, err := requestedPlatformInstanceOrder(body.Items)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "目录排序数据无效", map[string]any{})
		return
	}
	transaction, err := server.database.BeginTx(request.Context(), nil)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer cleanup.Rollback(transaction)
	current, err := readPlatformInstanceOrder(request.Context(), transaction, len(body.Items))
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if len(current) != len(requested) {
		writeError(writer, request, http.StatusConflict, "PLATFORM_INSTANCE_ORDER_STALE", "目录列表已变化，请刷新后重试", map[string]any{})
		return
	}
	for id, version := range requested {
		entry, exists := current[id]
		if !exists || entry.Version != version {
			writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "目录已被修改，请刷新后重试", map[string]any{})
			return
		}
	}
	now := server.now().UnixMilli()
	resultItems, err := applyPlatformInstanceOrder(request, transaction, body.Items, current, now)
	if errors.Is(err, errPlatformInstanceOrderVersion) {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "目录已被修改，请刷新后重试", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return
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
	current, err := server.readPlatformInstance(request, request.PathValue("platformInstanceId"))
	if err != nil {
		writeError(writer, request, http.StatusNotFound, "PLATFORM_INSTANCE_NOT_FOUND", "平台目录不存在", map[string]any{})
		return
	}
	values, ok := patchPlatformValues(current)
	if !ok {
		writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "平台目录投影无效", map[string]any{})
		return
	}
	if values.version != expected {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "平台目录已被修改", map[string]any{})
		return
	}
	values.apply(body)
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
SET name=?,
description=?,
sort_order=?,
enabled=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?
AND deleted_at_ms IS NULL
`,
		values.name,
		values.description,
		values.sortOrder,
		values.enabled,
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
	after := map[string]any{
		"name":        values.name,
		"description": values.description,
		"sortOrder":   values.sortOrder,
		"enabled":     values.enabled,
		"version":     expected + 1,
	}
	if err := insertAudit(
		request,
		transaction,
		"PLATFORM_INSTANCE_UPDATED",
		"PLATFORM_INSTANCE",
		request.PathValue("platformInstanceId"),
		current,
		after,
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
	after["id"] = request.PathValue("platformInstanceId")
	after["updatedAtMs"] = now
	writeJSON(writer, http.StatusOK, after)
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
	current, err := server.readPlatformInstance(request, request.PathValue("platformInstanceId"))
	if err != nil {
		writeError(writer, request, http.StatusNotFound, "PLATFORM_INSTANCE_NOT_FOUND", "平台目录不存在", map[string]any{})
		return
	}
	currentVersion, ok := current["version"].(int64)
	if !ok {
		writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "平台目录投影无效", map[string]any{})
		return
	}
	if currentVersion != expected {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "平台目录已被修改", map[string]any{})
		return
	}
	var count int
	if err := server.database.QueryRowContext(request.Context(), `
SELECT count(*)
FROM games
WHERE platform_instance_id=?
`, request.PathValue("platformInstanceId")).Scan(&count); err != nil ||
		count != 0 {
		writeError(writer, request, http.StatusConflict, "PLATFORM_INSTANCE_NOT_EMPTY", "非空目录不能删除", map[string]any{})
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
SET enabled=0,
deleted_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?
AND deleted_at_ms IS NULL
`,
		now,
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
		"PLATFORM_INSTANCE_DELETED",
		"PLATFORM_INSTANCE",
		request.PathValue("platformInstanceId"),
		current,
		map[string]any{"deletedAtMs": now, "version": expected + 1},
		now,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}
