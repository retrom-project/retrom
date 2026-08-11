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
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/contentprofile"
	"retrom/internal/cursor"

	"github.com/google/uuid"
)

var (
	errPlatformInstanceOrderInvalid = errors.New("invalid platform instance order")
	errPlatformInstanceOrderVersion = errors.New("platform instance order version conflict")
	errPlatformSlugExhausted        = errors.New("platform slug space exhausted")
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
	toSlug := func(value string) string {
		var builder strings.Builder
		separator := false
		for _, character := range value {
			if character >= 'A' && character <= 'Z' {
				character += 'a' - 'A'
			}
			if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
				if separator && builder.Len() > 0 && builder.Len() < 80 {
					builder.WriteByte('-')
				}
				separator = false
				if builder.Len() < 80 {
					builder.WriteRune(character)
				}
				continue
			}
			separator = builder.Len() > 0
		}
		return strings.TrimRight(builder.String(), "-")
	}
	if slug := toSlug(name); slug != "" {
		return slug
	}
	prefix := toSlug(platformID)
	if prefix == "" {
		prefix = "game"
	}
	return prefix + "-library"
}

func platformSlugWithSuffix(base string, suffix int) string {
	if suffix < 2 {
		return base
	}
	ending := "-" + strconv.Itoa(suffix)
	prefix := strings.TrimRight(base[:min(len(base), 80-len(ending))], "-")
	return prefix + ending
}

func nextPlatformSlug(ctx context.Context, transaction *sql.Tx, platformID, name string) (string, error) {
	base := platformSlugBase(name, platformID)
	prefix := base + "-"
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT slug FROM platform_instances WHERE platform_id=? AND (slug=? OR substr(slug,1,?)=?)`,
		platformID,
		base,
		len(prefix),
		prefix,
	)
	if err != nil {
		return "", fmt.Errorf("query platform slugs: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	used := make(map[string]struct{})
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return "", fmt.Errorf("scan platform slug: %w", err)
		}
		used[slug] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate platform slugs: %w", err)
	}
	for suffix := 1; suffix <= len(used)+1; suffix++ {
		candidate := platformSlugWithSuffix(base, suffix)
		if _, exists := used[candidate]; !exists {
			return candidate, nil
		}
	}
	return "", errPlatformSlugExhausted
}

//nolint:funlen // Request validation, uniqueness checks, creation, and audit write share one transaction.
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
	now := server.now().UnixMilli()
	id, err := uuid.NewV7()
	if err != nil {
		writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "无法创建目录", map[string]any{})
		return
	}
	transaction, err := server.database.BeginTx(request.Context(), nil)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer cleanup.Rollback(transaction)
	slug, err := nextPlatformSlug(request.Context(), transaction, body.PlatformID, body.Name)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	_, err = transaction.ExecContext(
		request.Context(),
		`
INSERT INTO platform_instances(id,
platform_id,
default_core_id,
name,
slug,
description,
sort_order,
enabled,
version,
created_at_ms,
updated_at_ms)
VALUES(?,
?,
?,
?,
?,
?,
?,
1,
1,
?,
?)
`,
		id.String(),
		body.PlatformID,
		body.DefaultCoreID,
		body.Name,
		slug,
		body.Description,
		body.SortOrder,
		now,
		now,
	)
	if err != nil {
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
	if err := insertAudit(
		request,
		transaction,
		"PLATFORM_INSTANCE_CREATED",
		"PLATFORM_INSTANCE",
		id.String(),
		nil,
		map[string]any{
			"platformId": body.PlatformID, "defaultCoreId": body.DefaultCoreID, "name": body.Name,
			"slug": slug, "description": body.Description, "sortOrder": body.SortOrder,
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
	writer.Header().Set("ETag", `"v1"`)
	writeJSON(
		writer,
		http.StatusCreated,
		map[string]any{
			"id":                  id.String(),
			"platformId":          body.PlatformID,
			"defaultCoreId":       body.DefaultCoreID,
			"name":                body.Name,
			"slug":                slug,
			"description":         body.Description,
			"sortOrder":           body.SortOrder,
			"enabled":             true,
			"gameCount":           0,
			"supportedExtensions": contentprofile.SupportedExtensions(body.PlatformID),
			"version":             1,
			"createdAtMs":         now,
			"updatedAtMs":         now,
		},
	)
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

//nolint:funlen,gocyclo // Contract branches stay contiguous for a single auditable decision.
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
	if err := decodeJSON(writer, request, &body, 32<<10); err != nil ||
		body.Name == nil && body.Description == nil && body.SortOrder == nil && body.Enabled == nil ||
		body.Name != nil && !validText(*body.Name, 1, 200, false) ||
		body.Description != nil && !validText(*body.Description, 0, 10_000, true) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "平台目录字段无效", map[string]any{})
		return
	}
	current, err := server.readPlatformInstance(request, request.PathValue("platformInstanceId"))
	if err != nil {
		writeError(writer, request, http.StatusNotFound, "PLATFORM_INSTANCE_NOT_FOUND", "平台目录不存在", map[string]any{})
		return
	}
	currentVersion, versionOK := current["version"].(int64)
	name, nameOK := current["name"].(string)
	description, descriptionOK := current["description"].(string)
	sortOrder, sortOK := current["sortOrder"].(int64)
	enabled, enabledOK := current["enabled"].(bool)
	if !versionOK || !nameOK || !descriptionOK || !sortOK || !enabledOK {
		writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "平台目录投影无效", map[string]any{})
		return
	}
	if currentVersion != expected {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "平台目录已被修改", map[string]any{})
		return
	}
	if body.Name != nil {
		name = *body.Name
	}
	if body.Description != nil {
		description = *body.Description
	}
	if body.SortOrder != nil {
		sortOrder = *body.SortOrder
	}
	if body.Enabled != nil {
		enabled = *body.Enabled
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
		name,
		description,
		sortOrder,
		enabled,
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
		"name":        name,
		"description": description,
		"sortOrder":   sortOrder,
		"enabled":     enabled,
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

//nolint:funlen // Reference checks, optimistic locking, deletion, and audit write share one transaction.
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

//nolint:funlen // Per-game readiness projection and blocker counts must be derived from one consistent query snapshot.
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
AND a.emulatorjs_version=?
AND a.enabled=1
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

//nolint:funlen // Impact digest validation, core switch, revalidation scheduling, and audit write are one transaction.
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
