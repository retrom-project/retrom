package httpapi

import (
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

//nolint:funlen // Aggregate and item projections are read together to preserve one import snapshot response.
func (server *Server) importDetail(writer http.ResponseWriter, request *http.Request) {
	var id, uploadID, targetID, targetName, platformID, coreID, artifactID, provider, state, configJSON string
	var datID, errorCode, cancelReason sql.NullString
	var version, total, queued, running, pending, published, discarded int64
	var failed, canceled, ignored, rejected, created, updated int64
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
i.last_error_code,
i.cancel_reason,
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
			&errorCode,
			&cancelReason,
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
	item := map[string]any{
		"importJobId":            id,
		"uploadId":               uploadID,
		"targetPlatformInstance": map[string]any{"id": targetID, "name": targetName},
		"platformId":             platformID,
		"defaultCoreId":          coreID,
		"coreArtifactId":         artifactID,
		"datVersionId":           nullableString(datID),
		"metadataProvider":       provider,
		"configSnapshot":         configValue,
		"state":                  state,
		"counts": map[string]any{
			"total":         total,
			"queued":        queued,
			"running":       running,
			"reviewPending": pending,
			"published":     published,
			"discarded":     discarded,
			"failed":        failed,
			"cancelled":     canceled,
			"ignoredFiles":  ignored,
			"rejectedFiles": rejected,
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
		Reason string `json:"reason"`
	}
	if decodeJSON(writer, request, &body, 8<<10) != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "丢弃原因无效", map[string]any{})
		return
	}
	result, err := server.importer.Discard(request.Context(), request.PathValue("importItemId"), version, body.Reason)
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
