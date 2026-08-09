package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/libraryimport"
)

func requireVersion(writer http.ResponseWriter, request *http.Request) (int64, bool) {
	version, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusPreconditionRequired,
			"PRECONDITION_REQUIRED",
			"需要当前资源版本",
			map[string]any{},
		)
		return 0, false
	}
	return version, true
}

func (server *Server) createReviewArcadeParentAttachment(
	writer http.ResponseWriter,
	request *http.Request,
) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body libraryimport.ParentAttachmentRequest
	if err := decodeJSON(writer, request, &body, 8<<10); err != nil {
		writeError(
			writer, request, http.StatusBadRequest, libraryimport.ParentErrorInvalid,
			"Parent ROM 上传请求无效", map[string]any{},
		)
		return
	}
	created, err := server.importer.CreateArcadeParentAttachment(
		request.Context(), request.PathValue("importItemId"), version, body,
	)
	if err != nil {
		code := libraryimport.ParentAttachmentErrorCode(err)
		status := http.StatusServiceUnavailable
		message := "Parent ROM 校验服务暂时不可用"
		switch code {
		case libraryimport.ParentErrorInvalid:
			status, message = http.StatusBadRequest, "Parent ROM 上传请求无效"
		case libraryimport.ParentErrorNotFound:
			status, message = http.StatusNotFound, "审核项不存在"
		case libraryimport.ParentErrorVersion:
			status, message = http.StatusConflict, "审核条目已发生变化"
		case libraryimport.ParentErrorInProgress:
			status, message = http.StatusConflict, "已有 Parent ROM 正在校验"
		case libraryimport.ParentErrorInputStale:
			status, message = http.StatusConflict, "运行验证输入已经变化"
		case libraryimport.ParentErrorFinalized:
			status, message = http.StatusConflict, "审核项已经完成决策"
		case libraryimport.ParentErrorNotRequired:
			status, message = http.StatusUnprocessableEntity, "当前依赖不需要此 Parent ROM"
		case libraryimport.ParentErrorStructure:
			status, message = http.StatusUnprocessableEntity, "当前 Arcade 结构不支持补充 Parent ROM"
		case libraryimport.ParentErrorArchiveUnsafe:
			status, message = http.StatusUnprocessableEntity, "Parent ROM 归档不安全"
		case libraryimport.ParentErrorMismatch:
			status, message = http.StatusUnprocessableEntity, "Parent ROM 内容与 DAT 不匹配"
		}
		writeError(writer, request, status, code, message, map[string]any{})
		return
	}
	writer.Header().Set("Location", "/api/v1/admin/jobs/"+created.JobID)
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, created.Version))
	writeJSON(writer, http.StatusAccepted, created)
}

//nolint:funlen // Aggregate and item projections are read together to preserve one import snapshot response.
func (server *Server) importDetail(writer http.ResponseWriter, request *http.Request) {
	var id, uploadID, targetID, targetName, platformID, coreID, artifactID, provider, state, configJSON string
	var datID, errorCode, cancelReason, reconfiguredFrom sql.NullString
	var version, total, queued, running, pending, published, discarded int64
	var failed, canceled, ignored, rejected, resolvedRejected, alreadyImportedItems, alreadyImportedFiles int64
	var created, updated int64
	err := server.database.QueryRowContext(request.Context(), `
SELECT i.id,
i.upload_session_id,
i.target_platform_instance_id,
p.name,
i.platform_id,
i.default_core_id,
i.core_artifact_id,
i.dat_version_id,
i.metadata_provider,
i.config_snapshot_json,
i.state,
i.total_item_count,
i.queued_item_count,
i.running_item_count,
i.review_pending_item_count,
i.published_item_count,
i.discarded_item_count,
i.failed_item_count,
i.cancelled_item_count,
i.ignored_file_count,
i.rejected_file_count,
i.resolved_rejected_file_count,
i.already_imported_item_count,
i.already_imported_file_count,
i.last_error_code,
i.cancel_reason,
i.reconfigured_from_import_job_id,
i.version,
i.created_at_ms,
i.updated_at_ms
FROM import_jobs i
JOIN platform_instances p ON p.id=i.target_platform_instance_id
WHERE i.id=?
`, request.PathValue("importJobId")).
		Scan(
			&id,
			&uploadID,
			&targetID,
			&targetName,
			&platformID,
			&coreID,
			&artifactID,
			&datID,
			&provider,
			&configJSON,
			&state,
			&total,
			&queued,
			&running,
			&pending,
			&published,
			&discarded,
			&failed,
			&canceled,
			&ignored,
			&rejected,
			&resolvedRejected,
			&alreadyImportedItems,
			&alreadyImportedFiles,
			&errorCode,
			&cancelReason,
			&reconfiguredFrom,
			&version,
			&created,
			&updated,
		)
	if errors.Is(err, sql.ErrNoRows) {
		server.notFound(writer, request)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var configValue any
	_ = json.Unmarshal([]byte(configJSON), &configValue)
	fileRows, err := server.database.QueryContext(request.Context(), `
SELECT u.id,
u.relative_path,
u.declared_size_bytes,
f.disposition,
f.reason_code,
resolution.action,
resolution.replacement_import_job_id,
resolution.created_at_ms,
EXISTS(
  SELECT 1
  FROM import_item_source_files source
  JOIN import_items item ON item.id=source.import_item_id
  JOIN import_item_duplicate_matches duplicate ON duplicate.import_item_id=item.id
  WHERE item.import_job_id=f.import_job_id
  AND source.upload_file_id=f.upload_file_id
)
AND NOT EXISTS(
  SELECT 1
  FROM import_item_source_files source
  JOIN import_items item ON item.id=source.import_item_id
  WHERE item.import_job_id=f.import_job_id
  AND source.upload_file_id=f.upload_file_id
  AND NOT EXISTS(
    SELECT 1 FROM import_item_duplicate_matches duplicate
    WHERE duplicate.import_item_id=item.id
  )
)
FROM import_job_files f
JOIN upload_files u ON u.id=f.upload_file_id
LEFT JOIN import_job_file_resolutions resolution
ON resolution.import_job_id=f.import_job_id
AND resolution.upload_file_id=f.upload_file_id
WHERE f.import_job_id=?
ORDER BY u.relative_path,u.id
`, id)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", fileRows.Close()) }()
	fileOutcomes := make([]map[string]any, 0)
	for fileRows.Next() {
		var uploadFileID, name, disposition string
		var sizeBytes int64
		var reasonCode, resolutionAction, replacementImportJobID sql.NullString
		var resolvedAtMS sql.NullInt64
		var alreadyImported int64
		if err := fileRows.Scan(
			&uploadFileID,
			&name,
			&sizeBytes,
			&disposition,
			&reasonCode,
			&resolutionAction,
			&replacementImportJobID,
			&resolvedAtMS,
			&alreadyImported,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		outcome := map[string]any{
			"uploadFileId": uploadFileID,
			"name":         name,
			"sizeBytes":    sizeBytes,
			"disposition":  disposition,
			"reasonCode":   nullableString(reasonCode),
			"resolution":   nil,
		}
		if alreadyImported == 1 {
			outcome["disposition"] = "ALREADY_IMPORTED"
			outcome["reasonCode"] = "ALREADY_IMPORTED"
		}
		if resolutionAction.Valid && replacementImportJobID.Valid && resolvedAtMS.Valid {
			outcome["resolution"] = map[string]any{
				"action":                 resolutionAction.String,
				"replacementImportJobId": replacementImportJobID.String,
				"resolvedAtMs":           resolvedAtMS.Int64,
			}
		}
		fileOutcomes = append(fileOutcomes, outcome)
	}
	if err := fileRows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := fileRows.Close(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	alreadyImportedMatches, err := server.importDuplicateMatches(request.Context(), id)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	item := map[string]any{
		"importJobId":                 id,
		"uploadId":                    uploadID,
		"targetPlatformInstance":      map[string]any{"id": targetID, "name": targetName},
		"platformId":                  platformID,
		"defaultCoreId":               coreID,
		"coreArtifactId":              artifactID,
		"datVersionId":                nullableString(datID),
		"metadataProvider":            provider,
		"reconfiguredFromImportJobId": nullableString(reconfiguredFrom),
		"configSnapshot":              configValue,
		"fileOutcomes":                fileOutcomes,
		"alreadyImportedMatches":      alreadyImportedMatches,
		"state":                       state,
		"counts": map[string]any{
			"total":                   total,
			"queued":                  queued,
			"running":                 running,
			"reviewPending":           pending,
			"published":               published,
			"discarded":               discarded,
			"failed":                  failed,
			"cancelled":               canceled,
			"ignoredFiles":            ignored,
			"rejectedFiles":           rejected,
			"unresolvedRejectedFiles": rejected - resolvedRejected,
			"alreadyImportedItems":    alreadyImportedItems,
			"alreadyImportedFiles":    alreadyImportedFiles,
		},
		"errorCode":    nullableString(errorCode),
		"cancelReason": nullableString(cancelReason),
		"version":      version,
		"createdAtMs":  created,
		"updatedAtMs":  updated,
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version))
	writeJSON(writer, http.StatusOK, item)
}

func (server *Server) importDuplicateMatches(ctx context.Context, importJobID string) ([]map[string]any, error) {
	duplicateRows, err := server.database.QueryContext(ctx, `
SELECT match.import_item_id,
match.content_identity_digest,
game.id,
metadata.title,
instance.id,
instance.name
FROM import_item_duplicate_matches match
JOIN import_items item ON item.id=match.import_item_id
JOIN games game ON game.id=match.existing_game_id
JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
WHERE item.import_job_id=?
ORDER BY item.created_at_ms,item.id,game.created_at_ms,game.id
`, importJobID)
	if err != nil {
		return nil, fmt.Errorf("query import duplicate matches: %w", err)
	}
	defer func() { cleanup.Error("close", duplicateRows.Close()) }()
	alreadyImportedMatches := make([]map[string]any, 0)
	for duplicateRows.Next() {
		var importItemID, contentIdentityDigest, gameID, title, instanceID, instanceName string
		if err := duplicateRows.Scan(
			&importItemID,
			&contentIdentityDigest,
			&gameID,
			&title,
			&instanceID,
			&instanceName,
		); err != nil {
			return nil, fmt.Errorf("scan import duplicate match: %w", err)
		}
		alreadyImportedMatches = append(alreadyImportedMatches, map[string]any{
			"importItemId":          importItemID,
			"contentIdentityDigest": contentIdentityDigest,
			"existingGame": map[string]any{
				"id": gameID, "title": title,
				"platformInstanceId": instanceID, "platformInstanceName": instanceName,
			},
		})
	}
	if err := duplicateRows.Err(); err != nil {
		return nil, fmt.Errorf("scan import duplicate matches: %w", err)
	}
	return alreadyImportedMatches, nil
}

func (server *Server) reconfigureImport(writer http.ResponseWriter, request *http.Request) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body libraryimport.ReconfigureRequest
	if err := decodeJSON(writer, request, &body, 64<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "重新导入配置无效", map[string]any{})
		return
	}
	created, err := server.importer.Reconfigure(
		request.Context(),
		request.PathValue("importJobId"),
		version,
		body,
	)
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusConflict,
			"IMPORT_RECONFIGURE_CONFLICT",
			"任务状态、版本或待处理文件已经变化",
			map[string]any{},
		)
		return
	}
	writer.Header().Set("Location", "/api/v1/admin/imports/"+created.ImportJobID)
	writeJSON(writer, http.StatusAccepted, created)
}

func (server *Server) importEvents(writer http.ResponseWriter, request *http.Request) {
	server.streamAggregateEvents(writer, request, request.PathValue("importJobId"))
}

func (server *Server) cancelImport(writer http.ResponseWriter, request *http.Request) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if decodeJSON(writer, request, &body, 8<<10) != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "取消原因无效", map[string]any{})
		return
	}
	result, pending, err := server.importer.Cancel(
		request.Context(),
		request.PathValue("importJobId"),
		version,
		body.Reason,
	)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "IMPORT_CANCEL_CONFLICT", "导入任务状态或版本已经变化", map[string]any{})
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	status := http.StatusOK
	if pending {
		status = http.StatusAccepted
	}
	writeJSON(writer, status, result)
}

func (server *Server) retryImportItem(writer http.ResponseWriter, request *http.Request) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct{}
	if decodeJSON(writer, request, &body, 1024) != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "重试请求无效", map[string]any{})
		return
	}
	result, err := server.importer.RetryItem(request.Context(), request.PathValue("importItemId"), version)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "IMPORT_ITEM_NOT_RETRYABLE", "条目不可重试或版本已经变化", map[string]any{})
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writeJSON(writer, http.StatusAccepted, result)
}

func (server *Server) patchReview(writer http.ResponseWriter, request *http.Request) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	var body libraryimport.DraftPatch
	if decodeJSON(writer, request, &body, 64<<10) != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "审核草稿无效", map[string]any{})
		return
	}
	result, err := server.importer.PatchDraft(request.Context(), request.PathValue("importItemId"), version, body)
	if errors.Is(err, libraryimport.ErrReimportRequiredPlatformChange) {
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"REIMPORT_REQUIRED_FOR_PLATFORM_CHANGE",
			"跨基础平台会改变分组与识别证据，请丢弃后按目标目录重新导入",
			map[string]any{},
		)
		return
	}
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"REVIEW_DRAFT_INVALID",
			"草稿字段、归属或版本无效",
			map[string]any{},
		)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) scrapeReview(writer http.ResponseWriter, request *http.Request) {
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
	if err := decodeJSON(writer, request, &body, 64<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "刮削请求无效", map[string]any{})
		return
	}
	scheduled, version, err := server.metadata.ScheduleReview(
		request.Context(),
		request.PathValue("importItemId"),
		expected,
		body.MetadataProvider,
	)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "REVIEW_VERSION_CONFLICT", "审核条目已发生变化", map[string]any{})
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version))
	status, state := http.StatusAccepted, "QUEUED"
	if scheduled.Noop {
		status, state = http.StatusCreated, "SUCCEEDED"
	}
	writeJSON(
		writer,
		status,
		map[string]any{"scrapeRunId": scheduled.RunID, "jobId": scheduled.JobID, "state": state, "version": version},
	)
}

func (server *Server) discardReview(writer http.ResponseWriter, request *http.Request) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct {
		Reason *string `json:"reason"`
	}
	if decodeJSON(writer, request, &body, 8<<10) != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "丢弃请求无效", map[string]any{})
		return
	}
	reason := ""
	if body.Reason != nil {
		reason = *body.Reason
	}
	result, err := server.importer.Discard(request.Context(), request.PathValue("importItemId"), version, reason)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "REVIEW_DECISION_CONFLICT", "审核状态或版本已经变化", map[string]any{})
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) reviewHistory(writer http.ResponseWriter, request *http.Request) {
	query := `
SELECT e.id,
e.import_item_id,
e.event_type,
e.reason,
e.created_at_ms,
COALESCE(json_extract(d.metadata_json,
'$.title'),
''),
i.import_job_id
FROM review_events e
JOIN import_items i ON i.id=e.import_item_id
LEFT JOIN review_drafts d ON d.import_item_id=i.id
WHERE e.event_type IN ('APPROVED',
'DISCARDED')
`
	arguments := []any{}
	if normalizedQ := strings.ToLower(strings.Join(strings.Fields(request.URL.Query().Get("q")), " ")); normalizedQ != "" {
		query += " AND (instr(lower(COALESCE(json_extract(d.metadata_json,'$.title'),'')),?)>0 OR instr(i.search_text,?)>0)"
		arguments = append(arguments, normalizedQ, normalizedQ)
	}
	if decision := request.URL.Query().Get("decision"); decision != "" {
		if decision != "APPROVED" && decision != "DISCARDED" {
			writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "审核决定筛选无效", map[string]any{})
			return
		}
		query += " AND e.event_type=?"
		arguments = append(arguments, decision)
	}
	query += " ORDER BY e.created_at_ms DESC,e.id DESC LIMIT 100"
	rows, err := server.database.QueryContext(request.Context(), query, arguments...)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := []map[string]any{}
	for rows.Next() {
		var id, itemID, decision, title, importID string
		var reason sql.NullString
		var created int64
		if err := rows.Scan(&id, &itemID, &decision, &reason, &created, &title, &importID); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(
			items,
			map[string]any{
				"reviewEventId": id,
				"importItemId":  itemID,
				"importJobId":   importID,
				"title":         title,
				"decision":      decision,
				"reason":        nullableString(reason),
				"createdAtMs":   created,
			},
		)
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

func (server *Server) reviewHistoryEvent(writer http.ResponseWriter, request *http.Request) {
	var id, itemID, eventType, actor, before, after, diff, config, dat, provider string
	var reason sql.NullString
	var created int64
	err := server.database.QueryRowContext(request.Context(), `
SELECT id,
import_item_id,
event_type,
actor,
before_json,
after_json,
diff_json,
config_evidence_json,
dat_evidence_json,
provider_evidence_json,
reason,
created_at_ms
FROM review_events
WHERE id=?
AND event_type IN ('APPROVED',
'DISCARDED')
`, request.PathValue("reviewEventId")).
		Scan(&id, &itemID, &eventType, &actor, &before, &after, &diff, &config, &dat, &provider, &reason, &created)
	if errors.Is(err, sql.ErrNoRows) {
		server.notFound(writer, request)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	decode := func(value string) any { var result any; _ = json.Unmarshal([]byte(value), &result); return result }
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"reviewEventId":    id,
			"importItemId":     itemID,
			"eventType":        eventType,
			"actor":            actor,
			"before":           decode(before),
			"after":            decode(after),
			"diff":             decode(diff),
			"configEvidence":   decode(config),
			"datEvidence":      decode(dat),
			"providerEvidence": decode(provider),
			"reason":           nullableString(reason),
			"createdAtMs":      created,
		},
	)
}
