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
	"retrom/internal/composition"
	gamemove "retrom/internal/service/gamemove"
)

type gameMoveImpact = gamemove.Impact

// Contract branches stay contiguous for a single auditable decision.
func (server *Server) calculateMoveImpact(
	request *http.Request,
	targetID string,
	expected int64,
) (gameMoveImpact, error) {
	impact, err := composition.NewGameMove(server.database).Preview(request.Context(), gamemove.PreviewRequest{
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
		pending, ensureErr := server.launcher.EnsureVariantForMove(
			request.Context(),
			request.PathValue("gameId"),
			impact.TargetCoreID,
		)
		if ensureErr != nil {
			writeError(
				writer,
				request,
				http.StatusConflict,
				"VARIANT_VALIDATION_FAILED",
				"目标核心验证无法创建或已失败",
				map[string]any{},
			)
			return
		}
		if pending.Status == "VALIDATION_PENDING" {
			writeJSON(
				writer,
				http.StatusAccepted,
				map[string]any{"status": pending.Status, "jobId": pending.JobID, "retryAfterMs": pending.RetryAfterMS},
			)
			server.resumeMoveValidationAfterIdempotency(context.WithoutCancel(request.Context()), pending.JobID)
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
	writeJSON(writer, http.StatusOK, map[string]any{"impact": impact, "impactDigest": moveDigest(impact)})
}

func (server *Server) resumeMoveValidationAfterIdempotency(ctx context.Context, jobID string) {
	go func() {
		// Move preview responses are persisted while this mutex is held. Let
		// requests already queued for that mutex observe the queued Job before a
		// very small validation can become READY.
		server.waitForQueuedIdempotentRequests()
		server.idempotency.Lock()
		state, err := composition.NewGameMove(server.database).QueuedJobState(ctx, jobID)
		server.idempotency.Unlock()
		if err == nil && state == "QUEUED" {
			server.launcher.ResumeValidationJob(ctx, jobID)
		}
	}()
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
	result, err := composition.NewGameMove(server.database).Move(request.Context(), gamemove.MoveRequest{
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
	scheduled, version, err := server.metadata.ScheduleGame(request.Context(), request.PathValue("gameId"), expected)
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusConflict,
			"VERSION_CONFLICT",
			"游戏内容或版本已经变化",
			map[string]any{},
		)
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
	result, err := composition.NewGameMove(server.database).ScrapeCandidates(
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
				"gameId": request.PathValue("gameId"), "scrapeRunId": nil, "items": []any{},
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
			"gameId": request.PathValue("gameId"), "scrapeRunId": *result.RunID, "items": items,
		},
	)
}
