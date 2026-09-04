package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/libraryimport"
	"retrom/internal/tagging"
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

func requireReviewAttachmentWrite(writer http.ResponseWriter, request *http.Request) (int64, bool) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return 0, false
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return 0, false
	}
	return version, true
}

type reviewAttachmentCreated struct {
	jobID, responseVersion string
	response               any
}

type attachmentHTTPError struct {
	status  int
	message string
}

func handleReviewAttachment[Request any](
	writer http.ResponseWriter,
	request *http.Request,
	invalidCode, invalidMessage, unavailableMessage string,
	errorCode func(error) string,
	errorMappings map[string]attachmentHTTPError,
	create func(context.Context, string, int64, Request) (reviewAttachmentCreated, error),
) {
	version, ok := requireReviewAttachmentWrite(writer, request)
	if !ok {
		return
	}
	var body Request
	if err := decodeJSON(writer, request, &body, 8<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, invalidCode, invalidMessage, map[string]any{})
		return
	}
	created, err := create(request.Context(), request.PathValue("importItemId"), version, body)
	if err != nil {
		code := errorCode(err)
		mapped, exists := errorMappings[code]
		if !exists {
			mapped = attachmentHTTPError{status: http.StatusServiceUnavailable, message: unavailableMessage}
		}
		writeError(writer, request, mapped.status, code, mapped.message, map[string]any{})
		return
	}
	writer.Header().Set("Location", "/api/v1/admin/jobs/"+created.jobID)
	writer.Header().Set("ETag", created.responseVersion)
	writeJSON(writer, http.StatusAccepted, created.response)
}

var arcadeParentAttachmentErrors = map[string]attachmentHTTPError{
	libraryimport.ParentErrorInvalid:       {http.StatusBadRequest, "Parent ROM 上传请求无效"},
	libraryimport.ParentErrorNotFound:      {http.StatusNotFound, "审核项不存在"},
	libraryimport.ParentErrorVersion:       {http.StatusConflict, "审核条目已发生变化"},
	libraryimport.ParentErrorInProgress:    {http.StatusConflict, "已有 Parent ROM 正在校验"},
	libraryimport.ParentErrorInputStale:    {http.StatusConflict, "运行验证输入已经变化"},
	libraryimport.ParentErrorFinalized:     {http.StatusConflict, "审核项已经完成决策"},
	libraryimport.ParentErrorNotRequired:   {http.StatusUnprocessableEntity, "当前依赖不需要此 Parent ROM"},
	libraryimport.ParentErrorStructure:     {http.StatusUnprocessableEntity, "当前 Arcade 结构不支持补充 Parent ROM"},
	libraryimport.ParentErrorArchiveUnsafe: {http.StatusUnprocessableEntity, "Parent ROM 归档不安全"},
	libraryimport.ParentErrorMismatch:      {http.StatusUnprocessableEntity, "Parent ROM 内容与 DAT 不匹配"},
}

var multiDiscAttachmentErrors = map[string]attachmentHTTPError{
	libraryimport.MultiDiscAttachmentErrorInvalid:         {http.StatusBadRequest, "缺失光盘上传请求无效"},
	libraryimport.MultiDiscAttachmentErrorNotFound:        {http.StatusNotFound, "审核项不存在"},
	libraryimport.MultiDiscAttachmentErrorVersion:         {http.StatusConflict, "审核条目已发生变化"},
	libraryimport.MultiDiscAttachmentErrorInProgress:      {http.StatusConflict, "已有缺失光盘正在校验"},
	libraryimport.MultiDiscAttachmentErrorRetryRequired:   {http.StatusConflict, "请先重试或取消失败的校验任务"},
	libraryimport.MultiDiscAttachmentErrorInputStale:      {http.StatusConflict, "多盘验证输入已经变化"},
	libraryimport.MultiDiscAttachmentErrorFinalized:       {http.StatusConflict, "审核项已经完成决策"},
	libraryimport.MultiDiscAttachmentErrorContentInvalid:  {http.StatusUnprocessableEntity, "多盘内容无效或当前无需补传"},
	libraryimport.MultiDiscAttachmentErrorSetMismatch:     {http.StatusUnprocessableEntity, "上传文件与全部缺失光盘不一致"},
	libraryimport.MultiDiscAttachmentErrorModeUnavailable: {http.StatusUnprocessableEntity, "当前平台或核心不支持多盘内容"},
}

func (server *Server) createReviewArcadeParentAttachment(writer http.ResponseWriter, request *http.Request) {
	handleReviewAttachment(
		writer, request, libraryimport.ParentErrorInvalid, "Parent ROM 上传请求无效",
		"Parent ROM 校验服务暂时不可用", libraryimport.ParentAttachmentErrorCode,
		arcadeParentAttachmentErrors,
		func(ctx context.Context, itemID string, version int64, body libraryimport.ParentAttachmentRequest) (
			reviewAttachmentCreated, error,
		) {
			created, err := server.importer.CreateArcadeParentAttachment(ctx, itemID, version, body)
			if err != nil {
				return reviewAttachmentCreated{}, fmt.Errorf("create arcade parent attachment: %w", err)
			}
			return reviewAttachmentCreated{
				jobID: created.JobID, responseVersion: fmt.Sprintf(`"v%d"`, created.Version), response: created,
			}, nil
		},
	)
}

func (server *Server) createReviewMultiDiscAttachment(writer http.ResponseWriter, request *http.Request) {
	handleReviewAttachment(
		writer, request, libraryimport.MultiDiscAttachmentErrorInvalid, "缺失光盘上传请求无效",
		"多盘校验服务暂时不可用", libraryimport.MultiDiscAttachmentErrorCode,
		multiDiscAttachmentErrors,
		func(ctx context.Context, itemID string, version int64, body libraryimport.MultiDiscAttachmentRequest) (
			reviewAttachmentCreated, error,
		) {
			created, err := server.importer.CreateMultiDiscAttachment(ctx, itemID, version, body)
			if err != nil {
				return reviewAttachmentCreated{}, fmt.Errorf("create multi-disc attachment: %w", err)
			}
			return reviewAttachmentCreated{
				jobID:           created.JobID,
				responseVersion: fmt.Sprintf(`"v%d"`, created.ReviewVersion),
				response:        created,
			}, nil
		},
	)
}

type importMultiDiscItemSummary struct {
	ItemID           string   `json:"itemId"`
	State            string   `json:"state"`
	ContentKind      string   `json:"contentKind"`
	Playlist         string   `json:"playlist"`
	DiscCount        int64    `json:"discCount"`
	PresentDiscCount int64    `json:"presentDiscCount"`
	MissingDiscCount int64    `json:"missingDiscCount"`
	IgnoredFileCount int      `json:"ignoredFileCount"`
	IgnoredFiles     []string `json:"ignoredFiles"`
	playlistPath     string
}

func (server *Server) importMultiDiscItemSummaries(
	ctx context.Context,
	importJobID string,
) ([]importMultiDiscItemSummary, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT item.id,item.state,snapshot.content_kind,playlist.logical_name,upload.relative_path,
count(entry.ordinal),coalesce(sum(entry.state='PRESENT'),0),coalesce(sum(entry.state='MISSING'),0)
FROM import_items item
JOIN import_item_source_snapshots snapshot ON snapshot.import_item_id=item.id
AND snapshot.revision_no=(
  SELECT max(candidate.revision_no) FROM import_item_source_snapshots candidate
  WHERE candidate.import_item_id=item.id
)
JOIN import_item_source_snapshot_files playlist ON playlist.source_snapshot_id=snapshot.id
AND playlist.role='PLAYLIST_SOURCE'
JOIN upload_files upload ON upload.id=playlist.upload_file_id
LEFT JOIN import_item_multidisc_entries entry ON entry.source_snapshot_id=snapshot.id
WHERE item.import_job_id=? AND snapshot.content_kind='MULTI_DISC'
GROUP BY item.id,item.state,snapshot.content_kind,playlist.logical_name,upload.relative_path
ORDER BY upload.relative_path,item.id
`, importJobID)
	if err != nil {
		return nil, fmt.Errorf("query multi-disc item summaries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	summaries := make([]importMultiDiscItemSummary, 0)
	for rows.Next() {
		var summary importMultiDiscItemSummary
		if err := rows.Scan(
			&summary.ItemID,
			&summary.State,
			&summary.ContentKind,
			&summary.Playlist,
			&summary.playlistPath,
			&summary.DiscCount,
			&summary.PresentDiscCount,
			&summary.MissingDiscCount,
		); err != nil {
			return nil, fmt.Errorf("scan multi-disc item summary: %w", err)
		}
		summary.IgnoredFiles = make([]string, 0)
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate multi-disc item summaries: %w", err)
	}
	ignoredRows, err := server.database.QueryContext(ctx, `
SELECT upload.relative_path
FROM import_job_files outcome
JOIN upload_files upload ON upload.id=outcome.upload_file_id
WHERE outcome.import_job_id=? AND outcome.disposition='IGNORED'
ORDER BY upload.relative_path,upload.id
`, importJobID)
	if err != nil {
		return nil, fmt.Errorf("query multi-disc ignored files: %w", err)
	}
	defer func() { cleanup.Error("close", ignoredRows.Close()) }()
	ignoredByDirectory := make(map[string][]string)
	for ignoredRows.Next() {
		var relativePath string
		if err := ignoredRows.Scan(&relativePath); err != nil {
			return nil, fmt.Errorf("scan multi-disc ignored file: %w", err)
		}
		directory := path.Dir(relativePath)
		ignoredByDirectory[directory] = append(ignoredByDirectory[directory], path.Base(relativePath))
	}
	if err := ignoredRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate multi-disc ignored files: %w", err)
	}
	for index := range summaries {
		ignored := ignoredByDirectory[path.Dir(summaries[index].playlistPath)]
		summaries[index].IgnoredFileCount = len(ignored)
		if len(ignored) > 20 {
			ignored = ignored[:20]
		}
		summaries[index].IgnoredFiles = ignored
	}
	return summaries, nil
}

// Aggregate and item projections are read together to preserve one import snapshot response.
func (server *Server) importDetail(writer http.ResponseWriter, request *http.Request) {
	var id, uploadID, targetID, targetName, platformID, coreID, runtimeProviderID, runtimeTargetID string
	var metadataProvider, state, configJSON string
	var payloadState string
	var datID, errorCode, cancelReason, reconfiguredFrom, payloadReleaseJobID sql.NullString
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
i.provider_id,
i.target_id,
i.dat_version_id,
i.metadata_provider,
i.config_snapshot_json,
i.state,
i.payload_state,
i.payload_release_job_id,
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
			&runtimeProviderID,
			&runtimeTargetID,
			&datID,
			&metadataProvider,
			&configJSON,
			&state,
			&payloadState,
			&payloadReleaseJobID,
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
	fileOutcomes, err := server.importFileOutcomes(request.Context(), id)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	alreadyImportedMatches, err := server.importDuplicateMatches(request.Context(), id)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	itemSummaries, err := server.importMultiDiscItemSummaries(request.Context(), id)
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
		"providerId":                  runtimeProviderID,
		"targetId":                    runtimeTargetID,
		"datVersionId":                nullableString(datID),
		"metadataProvider":            metadataProvider,
		"reconfiguredFromImportJobId": nullableString(reconfiguredFrom),
		"configSnapshot":              configValue,
		"fileOutcomes":                fileOutcomes,
		"alreadyImportedMatches":      alreadyImportedMatches,
		"itemSummaries":               itemSummaries,
		"state":                       state,
		"payloadState":                payloadState,
		"payloadReleaseJobId":         nullableString(payloadReleaseJobID),
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

func (server *Server) importFileOutcomes(ctx context.Context, importJobID string) ([]map[string]any, error) {
	fileRows, err := server.database.QueryContext(ctx, `
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
`, importJobID)
	if err != nil {
		return nil, fmt.Errorf("query import file outcomes: %w", err)
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
			return nil, fmt.Errorf("scan import file outcome: %w", err)
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
		return nil, fmt.Errorf("scan import file outcomes: %w", err)
	}
	if err := fileRows.Close(); err != nil {
		return nil, fmt.Errorf("close import file outcomes: %w", err)
	}
	return fileOutcomes, nil
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
JOIN games metadata ON metadata.id=game.id
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
	if body.TagIDs != nil {
		if _, err := tagging.ValidateIDs(body.TagIDs); err != nil {
			writeTagError(writer, request, err)
			return
		}
	}
	created, err := server.importer.Reconfigure(
		request.Context(),
		request.PathValue("importJobId"),
		version,
		body,
	)
	if err != nil {
		if errors.Is(err, tagging.ErrReferenceInvalid) || errors.Is(err, tagging.ErrAssignmentLimitExceeded) {
			writeTagError(writer, request, err)
			return
		}
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
	if _, err := tagging.ValidateIDs(body.TagIDs); err != nil {
		writeTagError(writer, request, err)
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
	if errors.Is(err, tagging.ErrReferenceInvalid) || errors.Is(err, tagging.ErrAssignmentLimitExceeded) {
		writeTagError(writer, request, err)
		return
	}
	if errors.Is(err, libraryimport.ErrVersionConflict) {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "审核条目版本已变化", map[string]any{})
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
	var id, itemID, eventType, actorKind, before, after, diff, config, dat, provider string
	var actorUserID, actorLabel, reason sql.NullString
	var created int64
	err := server.database.QueryRowContext(request.Context(), `
SELECT id,
import_item_id,
event_type,
actor_kind,
actor_user_id,
actor_label,
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
		Scan(
			&id,
			&itemID,
			&eventType,
			&actorKind,
			&actorUserID,
			&actorLabel,
			&before,
			&after,
			&diff,
			&config,
			&dat,
			&provider,
			&reason,
			&created,
		)
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
			"reviewEventId": id,
			"importItemId":  itemID,
			"eventType":     eventType,
			"actor": map[string]any{
				"kind":   actorKind,
				"userId": nullableString(actorUserID),
				"label":  nullableString(actorLabel),
			},
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
