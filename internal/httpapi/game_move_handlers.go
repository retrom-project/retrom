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

	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
)

type gameMoveImpact struct {
	Action                   string   `json:"action"`
	GameID                   string   `json:"gameId"`
	GameVersion              int64    `json:"gameVersion"`
	ContentRevisionID        string   `json:"contentRevisionId"`
	SourcePlatformInstanceID string   `json:"sourcePlatformInstanceId"`
	TargetPlatformInstanceID string   `json:"targetPlatformInstanceId"`
	TargetPlatformVersion    int64    `json:"targetPlatformInstanceVersion"`
	TargetCoreID             string   `json:"targetCoreId"`
	TargetProviderID         string   `json:"targetProviderId"`
	TargetID                 string   `json:"targetId"`
	TargetContractSHA256     string   `json:"targetContractSha256"`
	TargetGameCompatibility  string   `json:"targetGameCompatibilityLine"`
	TargetDATVersionID       any      `json:"targetDatVersionId"`
	ValidationInputDigest    string   `json:"validationInputDigest"`
	VariantStatus            string   `json:"variantStatus"`
	BlockerCodes             []string `json:"blockerCodes"`
}

// Contract branches stay contiguous for a single auditable decision.
func (server *Server) calculateMoveImpact(
	request *http.Request,
	targetID string,
	expected int64,
) (gameMoveImpact, error) {
	var sourceID, sourcePlatform, contentID, contentLogicalName, targetPlatform, targetCore string
	var providerID, runtimeTargetID, targetContractSHA256, gameCompatibilityLine string
	var version, targetVersion int64
	var datID sql.NullString
	if err := server.database.QueryRowContext(request.Context(), `
SELECT g.platform_instance_id,
src.platform_id,
g.current_content_revision_id,
COALESCE(content.logical_name,''),
g.version,
target.platform_id,
target.default_core_id,
target.version,binding.provider_id,binding.target_id,runtime_target.target_contract_sha256,
runtime_target.game_compatibility_line,
(SELECT id
FROM dat_versions
WHERE provider_id=binding.provider_id AND target_id=binding.target_id
AND is_active=1)
FROM games g
JOIN platform_instances src ON src.id=g.platform_instance_id
LEFT JOIN game_content_files content ON content.game_content_revision_id=g.current_content_revision_id
AND content.role='CONTENT'
JOIN platform_instances target ON target.id=?
AND target.enabled=1
AND target.deleted_at_ms IS NULL
JOIN runtime_target_bindings binding ON binding.core_id=target.default_core_id
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=target.platform_id AND binding_platform.core_id=target.default_core_id
JOIN runtime_targets runtime_target ON runtime_target.provider_id=binding.provider_id
 AND runtime_target.target_id=binding.target_id
WHERE g.id=?
AND g.status='PUBLISHED'
`, targetID, request.PathValue("gameId")).Scan(
		&sourceID,
		&sourcePlatform,
		&contentID,
		&contentLogicalName,
		&version,
		&targetPlatform,
		&targetCore,
		&targetVersion,
		&providerID,
		&runtimeTargetID,
		&targetContractSHA256,
		&gameCompatibilityLine,
		&datID,
	); err != nil {
		return gameMoveImpact{}, fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	if version != expected || sourceID == targetID || sourcePlatform != targetPlatform {
		return gameMoveImpact{}, errStaleImpact
	}
	status, code := "NEEDS_VALIDATION", "VARIANT_VALIDATION_REQUIRED"
	biosSnapshot, _, _, err := corevalidation.ResolveBIOS(
		request.Context(),
		server.database,
		providerID,
		runtimeTargetID,
		contentLogicalName,
	)
	if err != nil {
		return gameMoveImpact{}, fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	inputDigest, err := corevalidation.ProviderValidationInputDigest(
		providerID, runtimeTargetID, targetContractSHA256, gameCompatibilityLine,
		contentID, datID, biosSnapshot,
	)
	if err != nil {
		return gameMoveImpact{}, fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	var storedStatus, storedCode string
	err = server.database.QueryRowContext(request.Context(), `
SELECT r.status,
r.compatibility_code
FROM game_variants v
JOIN game_variant_revisions r ON r.id=v.current_revision_id
WHERE v.game_id=?
AND v.core_id=?
AND r.validation_input_digest=?
`, request.PathValue("gameId"), targetCore, inputDigest).
		Scan(&storedStatus, &storedCode)
	switch {
	case err == nil:
		status, code = storedStatus, storedCode
	case errors.Is(err, sql.ErrNoRows):
		err = server.database.QueryRowContext(request.Context(), `
SELECT r.status,
r.compatibility_code
FROM game_variants v
JOIN game_variant_revisions r ON r.game_variant_id=v.id
WHERE v.game_id=?
AND v.core_id=?
AND r.validation_input_digest=?
ORDER BY r.created_at_ms DESC,
r.id DESC LIMIT 1
`, request.PathValue("gameId"), targetCore, inputDigest).
			Scan(&storedStatus, &storedCode)
		if err == nil && storedStatus != "READY" {
			status, code = storedStatus, storedCode
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return gameMoveImpact{}, fmt.Errorf("httpapi/game_handlers: %w", err)
		}
	default:
		return gameMoveImpact{}, fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	blockers := []string{}
	if status != "READY" {
		blockers = append(blockers, code)
	}
	return gameMoveImpact{
		Action:                   "MOVE_GAME",
		GameID:                   request.PathValue("gameId"),
		GameVersion:              version,
		ContentRevisionID:        contentID,
		SourcePlatformInstanceID: sourceID,
		TargetPlatformInstanceID: targetID,
		TargetPlatformVersion:    targetVersion,
		TargetCoreID:             targetCore,
		TargetProviderID:         providerID,
		TargetID:                 runtimeTargetID,
		TargetContractSHA256:     targetContractSHA256,
		TargetGameCompatibility:  gameCompatibilityLine,
		TargetDATVersionID:       nullableString(datID),
		ValidationInputDigest:    inputDigest,
		VariantStatus:            status,
		BlockerCodes:             blockers,
	}, nil
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
			writeError(writer, request, http.StatusConflict, "IMPACT_PREVIEW_STALE", "验证完成后移动输入已变化", map[string]any{})
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
		var state string
		err := server.database.QueryRowContext(ctx, `
SELECT state
FROM jobs
WHERE id=?
`, jobID).Scan(&state)
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
	transaction, err := server.database.BeginTx(request.Context(), nil)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer cleanup.Rollback(transaction)
	now := server.now().UnixMilli()
	result, err := transaction.ExecContext(
		request.Context(),
		`
UPDATE games
SET platform_instance_id=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?
`,
		body.TargetPlatformInstanceID,
		now,
		request.PathValue("gameId"),
		expected,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if err := insertAudit(
		request,
		transaction,
		"GAME_MOVED",
		"GAME",
		request.PathValue("gameId"),
		map[string]any{"platformInstanceId": impact.SourcePlatformInstanceID},
		map[string]any{
			"platformInstanceId": body.TargetPlatformInstanceID,
			"targetCoreId":       impact.TargetCoreID,
			"variantStatus":      impact.VariantStatus,
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
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected+1))
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"gameId":             request.PathValue("gameId"),
			"platformInstanceId": body.TargetPlatformInstanceID,
			"version":            expected + 1,
			"updatedAtMs":        now,
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
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "游戏只支持显式 Hasheous 重刮削", map[string]any{})
		return
	}
	scheduled, version, err := server.metadata.ScheduleGame(request.Context(), request.PathValue("gameId"), expected)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏内容或版本已经变化", map[string]any{})
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
	var runID, contentID string
	err := server.database.QueryRowContext(request.Context(), `
SELECT r.id,
r.game_content_revision_id
FROM metadata_scrape_runs r
JOIN games g ON g.id=r.game_id
AND g.current_content_revision_id=r.game_content_revision_id
WHERE r.game_id=?
AND r.provider='HASHEOUS'
AND r.state='COMPLETED'
ORDER BY r.created_at_ms DESC,
r.id DESC LIMIT 1
`, request.PathValue("gameId")).
		Scan(&runID, &contentID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(
			writer,
			http.StatusOK,
			map[string]any{
				"gameId":            request.PathValue("gameId"),
				"contentRevisionId": nil,
				"scrapeRunId":       nil,
				"items":             []any{},
			},
		)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	rows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT id,
provider_game_id,
normalized_metadata_json,
evidence_json,
created_at_ms,
(SELECT count(*)
FROM scrape_candidate_hits h
WHERE h.scrape_candidate_id=c.id)
FROM scrape_candidates c
WHERE scrape_run_id=?
ORDER BY created_at_ms,
id
`,
		runID,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	type candidateRecord struct {
		id, providerID, metadataJSON, evidenceJSON string
		createdAt, hitCount                        int64
	}
	records := make([]candidateRecord, 0)
	for rows.Next() {
		var record candidateRecord
		if err := rows.Scan(
			&record.id,
			&record.providerID,
			&record.metadataJSON,
			&record.evidenceJSON,
			&record.createdAt,
			&record.hitCount,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := rows.Close(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		var metadata, evidence map[string]any
		_ = json.Unmarshal([]byte(record.metadataJSON), &metadata)
		_ = json.Unmarshal([]byte(record.evidenceJSON), &evidence)
		assets, assetErr := server.reviewCandidateAssets(request, record.id)
		if assetErr != nil {
			server.databaseError(writer, request, assetErr)
			return
		}
		items = append(
			items,
			map[string]any{
				"candidateId":    record.id,
				"providerGameId": record.providerID,
				"metadata":       metadata,
				"evidence":       evidence,
				"assets":         assets,
				"hitCount":       record.hitCount,
				"createdAtMs":    record.createdAt,
			},
		)
	}
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"gameId":            request.PathValue("gameId"),
			"contentRevisionId": contentID,
			"scrapeRunId":       runID,
			"items":             items,
		},
	)
}
