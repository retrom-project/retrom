package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"retrom/internal/capability/security/authn"
	"retrom/internal/foundation/cursor"
	platforminstancemodel "retrom/internal/model/platforminstance"
	"retrom/internal/service/platforminstance"
)

type coreImpact = platforminstancemodel.CoreImpact

func impactDigest(value any) string {
	if impact, ok := value.(coreImpact); ok {
		return platforminstance.ImpactDigest(impact)
	}
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
	result, err := server.platformDirectories.CoreImpact(request.Context(), instanceID, coreID, expected)
	if errors.Is(err, platforminstancemodel.ErrImpactStale) {
		return coreImpact{}, nil, nil, errStaleImpact
	}
	if errors.Is(err, platforminstancemodel.ErrInvalidCore) {
		return coreImpact{}, nil, nil, errInvalidCore
	}
	if err != nil {
		return coreImpact{}, nil, nil, fmt.Errorf("httpapi/platform core impact: %w", err)
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, map[string]any{
			"gameId": item.GameID, "status": item.Status, "blockerCode": item.BlockerCode,
		})
	}
	return result.Impact, result.Counts, items, nil
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
	filterDigest := cursorFilterDigest(
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
		payload, decodeErr := server.decodeCursor(
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
		token, encodeErr := server.encodeCursor(
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
	actor := authn.ActorFromContext(request.Context(), "release-setup")
	requestID, _ := request.Context().Value(requestIDKey).(string)
	change, err := server.platformDirectories.ChangeDefaultCore(
		request.Context(), request.PathValue("platformInstanceId"), body.CoreID, expected,
		body.ImpactDigest, body.ConfirmBlocked, platforminstancemodel.AuditActor{
			Kind:      actor.Kind,
			UserID:    actor.UserID,
			Label:     actor.Label,
			RequestID: requestID,
		},
	)
	if errors.Is(err, platforminstancemodel.ErrImpactStale) {
		writeError(writer, request, http.StatusConflict, "IMPACT_PREVIEW_STALE", "目录或影响输入已变化", map[string]any{})
		return
	}
	if errors.Is(err, platforminstancemodel.ErrDefaultCoreBlocked) {
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
	if errors.Is(err, platforminstancemodel.ErrVersionConflict) {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "平台目录已被修改", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, change.Version))
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"id":            request.PathValue("platformInstanceId"),
			"defaultCoreId": body.CoreID,
			"version":       change.Version,
			"updatedAtMs":   change.UpdatedAtMS,
		},
	)
}
