package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"retrom/internal/adapter/integration/libraryimport"
	libraryservice "retrom/internal/model/libraryimport"
	taggingmodel "retrom/internal/model/tagging"
	taggingservice "retrom/internal/service/tagging"
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
	libraryimport.ParentErrorInvalid:     {http.StatusBadRequest, "Parent ROM 上传请求无效"},
	libraryimport.ParentErrorNotFound:    {http.StatusNotFound, "审核项不存在"},
	libraryimport.ParentErrorVersion:     {http.StatusConflict, "审核条目已发生变化"},
	libraryimport.ParentErrorInProgress:  {http.StatusConflict, "已有 Parent ROM 正在校验"},
	libraryimport.ParentErrorInputStale:  {http.StatusConflict, "运行验证输入已经变化"},
	libraryimport.ParentErrorFinalized:   {http.StatusConflict, "审核项已经完成决策"},
	libraryimport.ParentErrorNotRequired: {http.StatusUnprocessableEntity, "当前依赖不需要此 Parent ROM"},
	libraryimport.ParentErrorStructure: {
		http.StatusUnprocessableEntity,
		"当前 Arcade 结构不支持补充 Parent ROM",
	},
	libraryimport.ParentErrorArchiveUnsafe: {http.StatusUnprocessableEntity, "Parent ROM 归档不安全"},
	libraryimport.ParentErrorMismatch:      {http.StatusUnprocessableEntity, "Parent ROM 内容与 DAT 不匹配"},
}

var multiDiscAttachmentErrors = map[string]attachmentHTTPError{
	libraryimport.MultiDiscAttachmentErrorInvalid:    {http.StatusBadRequest, "缺失光盘上传请求无效"},
	libraryimport.MultiDiscAttachmentErrorNotFound:   {http.StatusNotFound, "审核项不存在"},
	libraryimport.MultiDiscAttachmentErrorVersion:    {http.StatusConflict, "审核条目已发生变化"},
	libraryimport.MultiDiscAttachmentErrorInProgress: {http.StatusConflict, "已有缺失光盘正在校验"},
	libraryimport.MultiDiscAttachmentErrorRetryRequired: {
		http.StatusConflict,
		"请先重试或取消失败的校验任务",
	},
	libraryimport.MultiDiscAttachmentErrorInputStale: {http.StatusConflict, "多盘验证输入已经变化"},
	libraryimport.MultiDiscAttachmentErrorFinalized:  {http.StatusConflict, "审核项已经完成决策"},
	libraryimport.MultiDiscAttachmentErrorContentInvalid: {
		http.StatusUnprocessableEntity,
		"多盘内容无效或当前无需补传",
	},
	libraryimport.MultiDiscAttachmentErrorSetMismatch: {
		http.StatusUnprocessableEntity,
		"上传文件与全部缺失光盘不一致",
	},
	libraryimport.MultiDiscAttachmentErrorModeUnavailable: {
		http.StatusUnprocessableEntity,
		"当前平台或核心不支持多盘内容",
	},
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

type importMultiDiscItemSummary = libraryservice.ImportMultiDiscItemSummary

func (server *Server) importMultiDiscItemSummaries(
	ctx context.Context,
	importJobID string,
) ([]importMultiDiscItemSummary, error) {
	result, err := server.importReads().MultiDiscItemSummaries(ctx, importJobID)
	if err != nil {
		return nil, fmt.Errorf("read multi-disc item summaries: %w", err)
	}
	return result, nil
}

// Aggregate and item projections are read together to preserve one import snapshot response.
func (server *Server) importDetail(writer http.ResponseWriter, request *http.Request) {
	item, err := server.importReads().Detail(request.Context(), request.PathValue("importJobId"))
	if errors.Is(err, libraryservice.ErrImportReadNotFound) {
		server.notFound(writer, request)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, item.Version))
	writeJSON(writer, http.StatusOK, item)
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
		if _, err := taggingservice.ValidateIDs(body.TagIDs); err != nil {
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
		if errors.Is(err, taggingmodel.ErrReferenceInvalid) || errors.Is(err, taggingmodel.ErrAssignmentLimitExceeded) {
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
		writeError(
			writer,
			request,
			http.StatusConflict,
			"IMPORT_CANCEL_CONFLICT",
			"导入任务状态或版本已经变化",
			map[string]any{},
		)
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
		writeError(
			writer,
			request,
			http.StatusConflict,
			"IMPORT_ITEM_NOT_RETRYABLE",
			"条目不可重试或版本已经变化",
			map[string]any{},
		)
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
	if _, err := taggingservice.ValidateIDs(body.TagIDs); err != nil {
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
	if errors.Is(err, taggingmodel.ErrReferenceInvalid) || errors.Is(err, taggingmodel.ErrAssignmentLimitExceeded) {
		writeTagError(writer, request, err)
		return
	}
	if errors.Is(err, libraryimport.ErrVersionConflict) {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "审核条目版本已变化", map[string]any{})
		return
	}
	if err != nil && !errors.Is(err, libraryimport.ErrInvalid) {
		server.databaseError(writer, request, err)
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
		writeError(
			writer,
			request,
			http.StatusConflict,
			"REVIEW_VERSION_CONFLICT",
			"审核条目已发生变化",
			map[string]any{},
		)
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

func (server *Server) reviewHistory(writer http.ResponseWriter, request *http.Request) {
	query := libraryservice.ReviewHistoryQuery{
		QueryText: strings.ToLower(strings.Join(strings.Fields(request.URL.Query().Get("q")), " ")),
		Decision:  request.URL.Query().Get("decision"),
	}
	if query.Decision != "" && query.Decision != "APPROVED" && query.Decision != "DISCARDED" {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "审核决定筛选无效", map[string]any{})
		return
	}
	items, err := server.importReads().ReviewHistory(request.Context(), query)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

func (server *Server) reviewHistoryEvent(writer http.ResponseWriter, request *http.Request) {
	event, err := server.importReads().ReviewHistoryEvent(
		request.Context(), request.PathValue("reviewEventId"),
	)
	if errors.Is(err, libraryservice.ErrImportReadNotFound) {
		server.notFound(writer, request)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"reviewEventId": event.ReviewEventID,
		"importItemId":  event.ImportItemID,
		"eventType":     event.EventType,
		"actor": map[string]any{
			"kind":   event.Actor.Kind,
			"userId": event.Actor.UserID,
			"label":  event.Actor.Label,
		},
		"before":           event.Before,
		"after":            event.After,
		"diff":             event.Diff,
		"configEvidence":   event.ConfigEvidence,
		"datEvidence":      event.DANEvidence,
		"providerEvidence": event.ProviderEvidence,
		"reason":           event.Reason,
		"createdAtMs":      event.CreatedAtMS,
	})
}
