package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"retrom/internal/authn"
	"retrom/internal/service/gamecontent"
)

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

func (server *Server) createGameContentReplacement(writer http.ResponseWriter, request *http.Request) {
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
		OperationID: "postAdminGameContentReplacement",
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
	detail, err := server.gameContent.AdminGame(request.Context(), request.PathValue("gameId"))
	if errors.Is(err, gamecontent.ErrAdminGameNotFound) {
		writeError(writer, request, http.StatusNotFound, "GAME_NOT_FOUND", "游戏不存在", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	impact, err := server.payloadReleases.GameDeleteImpact(request.Context(), request.PathValue("gameId"))
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	tags, err := server.activeGameTags(request.Context(), request.PathValue("gameId"))
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	assets := adminGameAssetsResponse(detail.Assets)
	files := adminGameFilesResponse(detail.Files)
	if detail.Status == "DELETED" {
		assets = []map[string]any{}
		files = []map[string]any{}
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, detail.Version))
	writeJSON(writer, http.StatusOK, map[string]any{
		"gameId": request.PathValue(
			"gameId",
		), "status": detail.Status, "payloadState": detail.PayloadState,
		"payloadReleaseJobId":  nullableGameString(detail.PayloadReleaseJobID),
		"payloadLastErrorCode": nullableGameString(detail.PayloadLastErrorCode),
		"title":                detail.Title, "description": detail.Description, "developer": detail.Developer,
		"publisher": detail.Publisher, "genre": detail.Genre,
		"players": nullableGameInteger(detail.Players), "releaseYear": nullableGameInteger(detail.ReleaseYear),
		"platformId":       detail.PlatformID,
		"platformInstance": map[string]any{"id": detail.InstanceID, "name": detail.InstanceName},
		"contentKind":      detail.ContentKind, "files": files, "version": detail.Version,
		"createdAtMs": detail.CreatedAtMS, "updatedAtMs": detail.UpdatedAtMS, "generatedAtMs": server.now().UnixMilli(),
		"deletedAtMs":  nullableGameInteger(detail.DeletedAtMS),
		"deleteImpact": impact, "assets": assets,
		"variants": adminGameVariantsResponse(detail.Variants), "tags": tags,
	})
}

func adminGameFilesResponse(files []gamecontent.AdminGameFile) []map[string]any {
	result := make([]map[string]any, 0, len(files))
	for _, file := range files {
		result = append(result, map[string]any{
			"role": file.Role, "logicalName": file.LogicalName, "sortOrder": file.SortOrder,
			"sizeBytes": file.SizeBytes, "sha256": file.SHA256,
		})
	}
	return result
}

func adminGameAssetsResponse(assets []gamecontent.AdminGameAsset) []map[string]any {
	result := make([]map[string]any, 0, len(assets))
	for _, asset := range assets {
		result = append(result, map[string]any{
			"assetId": asset.ID, "kind": asset.Kind, "ordinal": asset.Ordinal,
			"widthPx": nullableGameInteger(asset.WidthPX), "heightPx": nullableGameInteger(asset.HeightPX),
			"mediaType": asset.MediaType, "url": "/content/assets/" + asset.ID,
		})
	}
	return result
}

func adminGameVariantsResponse(variants []gamecontent.AdminGameVariant) []map[string]any {
	result := make([]map[string]any, 0, len(variants))
	for _, variant := range variants {
		result = append(result, map[string]any{
			"id": variant.ID, "coreId": variant.CoreID, "coreName": variant.CoreName,
			"providerId": nullableGameString(variant.ProviderID), "targetId": nullableGameString(variant.TargetID),
			"datVersionId": nullableGameString(variant.DATVersionID), "status": variant.Status,
			"compatibilityCode": variant.CompatibilityCode, "dependencySnapshot": variant.DependencySnapshot,
			"version": variant.Version, "createdAtMs": variant.CreatedAtMS, "updatedAtMs": variant.UpdatedAtMS,
		})
	}
	return result
}

func nullableGameString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableGameInteger(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
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
	actor := authn.ActorFromContext(request.Context(), "release-setup")
	requestID, _ := request.Context().Value(requestIDKey).(string)
	result, err := server.gameContent.PatchAdminGame(request.Context(), gamecontent.AdminGamePatchRequest{
		GameID:             request.PathValue("gameId"),
		ExpectedVersion:    expected,
		Title:              body.Title,
		Description:        body.Description,
		Developer:          body.Developer,
		Publisher:          body.Publisher,
		Genre:              body.Genre,
		PlayersPresent:     body.Players.Present,
		Players:            body.Players.Value,
		ReleaseYearPresent: body.ReleaseYear.Present,
		ReleaseYear:        body.ReleaseYear.Value,
		Actor: gamecontent.AuditActor{
			Kind: actor.Kind, UserID: actor.UserID, Label: actor.Label, RequestID: requestID,
		},
		NowMS: server.now().UnixMilli(),
	})
	if errors.Is(err, gamecontent.ErrAdminGameNotFound) {
		writeError(writer, request, http.StatusNotFound, "GAME_NOT_FOUND", "游戏不存在", map[string]any{})
		return
	}
	if errors.Is(err, gamecontent.ErrAdminGameVersionConflict) {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	server.payloadReleases.Signal()
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"gameId": request.PathValue("gameId"), "version": result.Version,
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
	principal := input.principal
	result, err := server.gameContent.DeleteAdminGame(request.Context(), gamecontent.DeleteGameRequest{
		GameID:          request.PathValue("gameId"),
		PrincipalID:     principal.UserID,
		Key:             request.Header.Get("Idempotency-Key"),
		RequestDigest:   input.requestDigest,
		ConfirmTitle:    input.body.ConfirmTitle,
		ImpactDigest:    input.body.ImpactDigest,
		ExpectedVersion: input.expected,
		Actor: func(ctx context.Context) gamecontent.AuditActor {
			actor := authn.ActorFromContext(ctx, "release-setup")
			return gamecontent.AuditActor{
				Kind: actor.Kind, UserID: actor.UserID, Label: actor.Label,
				RequestID: ctx.Value(requestIDKey),
			}
		}(request.Context()),
		NowMS: server.now().UnixMilli(),
	})
	switch {
	case errors.Is(err, gamecontent.ErrDeleteGameNotFound):
		writeError(writer, request, http.StatusNotFound, "GAME_NOT_FOUND", "游戏不存在", map[string]any{})
		return
	case errors.Is(err, gamecontent.ErrDeleteGameVersionConflict):
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	case errors.Is(err, gamecontent.ErrDeleteGameConfirmationMismatch):
		writeError(writer, request, http.StatusUnprocessableEntity,
			"GAME_DELETE_CONFIRMATION_MISMATCH", "确认标题不匹配", map[string]any{})
		return
	case errors.Is(err, gamecontent.ErrDeleteGameImpactStale):
		writeError(writer, request, http.StatusConflict,
			"GAME_DELETE_IMPACT_STALE", "删除影响已经变化，请刷新后重试", map[string]any{})
		return
	case err != nil:
		server.databaseError(writer, request, err)
		return
	}
	if result.Replayed {
		server.replayIdempotentResponse(
			writer, request, input.requestDigest,
			result.Replay.RequestDigest, result.Replay.HTTPStatus, result.Replay.HeadersJSON, result.Replay.Body,
		)
		return
	}
	writeStoredJSON(writer, result.HTTPStatus, result.ETag, result.Body)
}
