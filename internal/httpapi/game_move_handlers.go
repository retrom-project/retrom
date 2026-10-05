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

	"retrom/internal/authn"
	gamemove "retrom/internal/service/gamemove"
	"retrom/internal/service/gamevariant"
	"retrom/internal/service/idempotency"
	"retrom/internal/service/metadatascrape"
)

type gameMoveImpact = gamemove.Impact

// Contract branches stay contiguous for a single auditable decision.
func (server *Server) calculateMoveImpact(
	request *http.Request,
	targetID string,
	expected int64,
) (gameMoveImpact, error) {
	impact, err := server.libraryDeps.Moves.Preview(request.Context(), gamemove.PreviewRequest{
		GameID:                   request.PathValue("gameId"),
		TargetPlatformInstanceID: targetID,
		ExpectedVersion:          expected,
	})
	if err != nil {
		return gameMoveImpact{}, fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	return impact, nil
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
		pending, ensureErr := server.playDeps.Variants.Ensure(
			request.Context(),
			request.PathValue("gameId"),
			impact.TargetCoreID,
		)
		if ensureErr != nil {
			server.moveValidationError(writer, request, ensureErr)
			return
		}
		if !pending.Ready {
			writeJSON(
				writer,
				http.StatusAccepted,
				map[string]any{"status": "VALIDATION_PENDING", "jobId": pending.JobID, "retryAfterMs": pending.RetryAfterMS},
			)
			ctx := idempotency.WithoutCommand(context.WithoutCancel(request.Context()))
			afterIdempotencyCommit(writer, func() {
				server.resumeMoveValidation(ctx, pending.JobID)
			})
			return
		}
		impact, err = server.calculateMoveImpact(request, body.TargetPlatformInstanceID, expected)
		if err != nil || impact.VariantStatus == "NEEDS_VALIDATION" {
			writeError(
				writer,
				request,
				http.StatusConflict,
				"IMPACT_PREVIEW_STALE",
				"验证完成后移动输入已变化",
				map[string]any{},
			)
			return
		}
	}
	bodyResult := map[string]any{"impact": impact, "impactDigest": moveDigest(impact)}
	if err := idempotency.CompleteRead(request.Context(), idempotency.Result{Value: bodyResult}); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, bodyResult)
}

func (server *Server) resumeMoveValidation(ctx context.Context, jobID string) {
	server.deferredWork.Go(func() {
		state, err := server.libraryDeps.Moves.QueuedJobState(ctx, jobID)
		if err == nil && state == "QUEUED" {
			server.playDeps.Variants.Resume(ctx, jobID)
		}
	})
}

// Contract branches stay contiguous for a single auditable decision.
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
	now := server.now().UnixMilli()
	actor := authn.ActorFromContext(request.Context(), "release-setup")
	requestID, _ := request.Context().Value(requestIDKey).(string)
	result, err := server.libraryDeps.Moves.Move(request.Context(), gamemove.MoveRequest{
		GameID:                   request.PathValue("gameId"),
		TargetPlatformInstanceID: body.TargetPlatformInstanceID,
		ExpectedVersion:          expected,
		NowMS:                    now,
		Impact:                   impact,
		Actor: gamemove.AuditActor{
			Kind: actor.Kind, UserID: actor.UserID, Label: actor.Label, RequestID: requestID,
		},
	})
	if err != nil {
		if errors.Is(err, gamemove.ErrVersionConflict) {
			writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
			return
		}
		server.databaseError(writer, request, err)
		return
	}
	if result.Version != expected+1 {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"gameId":             result.GameID,
			"platformInstanceId": result.PlatformInstanceID,
			"version":            result.Version,
			"updatedAtMs":        result.UpdatedAtMS,
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
		writeError(
			writer,
			request,
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"游戏只支持显式 Hasheous 重刮削",
			map[string]any{},
		)
		return
	}
	scheduled, version, err := server.reviewDeps.Metadata.ScheduleGame(
		request.Context(), request.PathValue("gameId"), expected,
	)
	if err != nil {
		switch {
		case errors.Is(err, metadatascrape.ErrGameVersionConflict):
			writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏内容或版本已经变化", map[string]any{})
		case errors.Is(err, metadatascrape.ErrArchiveIndexMissing):
			writeError(writer, request, http.StatusConflict, "METADATA_ARCHIVE_INDEX_MISSING",
				"主 ROM 的归档索引缺失，无法查找游戏信息。请重新导入游戏后再试。", map[string]any{})
		default:
			server.databaseError(writer, request, err)
		}
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version))
	writeJSON(
		writer,
		http.StatusAccepted,
		map[string]any{"scrapeRunId": scheduled.RunID, "jobId": scheduled.JobID, "state": "QUEUED", "version": version},
	)
}

// Cursor validation and the candidate/evidence projection form one stable response contract.
func (server *Server) gameScrapeCandidates(writer http.ResponseWriter, request *http.Request) {
	result, err := server.libraryDeps.Moves.ScrapeCandidates(
		request.Context(), request.PathValue("gameId"),
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if result.RunID == nil {
		writeJSON(
			writer,
			http.StatusOK,
			map[string]any{
				"gameId": request.PathValue("gameId"), "scrapeRunId": nil, "evidenceCount": 0, "items": []any{},
			},
		)
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, record := range result.Items {
		var metadata, evidence map[string]any
		_ = json.Unmarshal([]byte(record.MetadataJSON), &metadata)
		_ = json.Unmarshal([]byte(record.EvidenceJSON), &evidence)
		assets, assetErr := server.reviewCandidateAssets(request, record.ID)
		if assetErr != nil {
			server.databaseError(writer, request, assetErr)
			return
		}
		items = append(
			items,
			map[string]any{
				"candidateId":    record.ID,
				"providerGameId": record.ProviderGameID,
				"metadata":       metadata,
				"evidence":       evidence,
				"assets":         assets,
				"hitCount":       record.HitCount,
				"createdAtMs":    record.CreatedAtMS,
			},
		)
	}
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"gameId": request.PathValue("gameId"), "scrapeRunId": *result.RunID,
			"evidenceCount": result.EvidenceCount, "items": items,
		},
	)
}

func (server *Server) moveValidationError(writer http.ResponseWriter, request *http.Request, err error) {
	if !errors.Is(err, gamevariant.ErrBlocked) {
		server.databaseError(writer, request, err)
		return
	}
	writeError(writer, request, http.StatusConflict, "VARIANT_VALIDATION_FAILED",
		"目标核心验证无法创建或已失败", map[string]any{})
}
