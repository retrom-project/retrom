package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"retrom/internal/libraryimport"
)

func parseReviewBulkScope(request *http.Request) (libraryimport.ReviewBulkScope, error) {
	values := request.URL.Query()
	allowed := map[string]struct{}{
		"q": {}, "tagId": {}, "importJobId": {}, "pegasusImportId": {},
		"emulationStationImportId": {},
		"platformInstanceId":       {}, "blockerCode": {},
	}
	for key := range values {
		if _, ok := allowed[key]; !ok {
			return libraryimport.ReviewBulkScope{}, errUnknownQuery
		}
		if len(values[key]) != 1 {
			return libraryimport.ReviewBulkScope{}, errUnknownQuery
		}
	}
	return libraryimport.ReviewBulkScope{
		Q: values.Get("q"), TagID: values.Get("tagId"), ImportJobID: values.Get("importJobId"),
		PegasusImportID:          values.Get("pegasusImportId"),
		EmulationStationImportID: values.Get("emulationStationImportId"),
		PlatformInstanceID:       values.Get("platformInstanceId"), BlockerCode: values.Get("blockerCode"),
	}, nil
}

func writeReviewBulkError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, libraryimport.ErrReviewBulkInvalidScope):
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "快速审批筛选无效", map[string]any{})
	case errors.Is(err, libraryimport.ErrReviewBulkPreviewStale):
		writeError(
			writer, request, http.StatusConflict, "REVIEW_BULK_PREVIEW_STALE",
			"待审核内容已经变化，请重新确认数量", map[string]any{},
		)
	case errors.Is(err, libraryimport.ErrReviewBulkActive):
		writeError(
			writer, request, http.StatusConflict, "REVIEW_BULK_APPROVAL_ACTIVE",
			"已有快速审批正在执行", map[string]any{},
		)
	case errors.Is(err, libraryimport.ErrReviewBulkTooLarge):
		writeError(
			writer, request, http.StatusUnprocessableEntity, "REVIEW_BULK_SCOPE_TOO_LARGE",
			"可自动发布的游戏超过 10000 个，请缩小筛选范围", map[string]any{},
		)
	case errors.Is(err, libraryimport.ErrReviewBulkEmpty):
		writeError(
			writer, request, http.StatusConflict, "REVIEW_BULK_SCOPE_EMPTY",
			"当前范围没有可自动发布的游戏", map[string]any{},
		)
	case errors.Is(err, libraryimport.ErrReviewBulkConflict):
		writeError(
			writer, request, http.StatusConflict, "REVIEW_BULK_VERSION_CONFLICT",
			"快速审批状态已经变化", map[string]any{},
		)
	case errors.Is(err, sql.ErrNoRows):
		serverNotFound(writer, request)
	default:
		writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "快速审批暂时不可用", map[string]any{})
	}
}

func serverNotFound(writer http.ResponseWriter, request *http.Request) {
	writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "资源不存在", map[string]any{})
}

func (server *Server) reviewBulkPreview(writer http.ResponseWriter, request *http.Request) {
	scope, err := parseReviewBulkScope(request)
	if err != nil {
		writeReviewBulkError(writer, request, libraryimport.ErrReviewBulkInvalidScope)
		return
	}
	preview, err := server.importer.PreviewReviewBulk(request.Context(), scope)
	if err != nil {
		writeReviewBulkError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, preview)
}

func (server *Server) createReviewBulk(writer http.ResponseWriter, request *http.Request) {
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body libraryimport.ReviewBulkCreateRequest
	if err := decodeJSON(writer, request, &body, 16<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "快速审批确认无效", map[string]any{})
		return
	}
	created, err := server.importer.CreateReviewBulk(request.Context(), body)
	if err != nil {
		writeReviewBulkError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", `"v1"`)
	writeJSON(writer, http.StatusAccepted, created)
}

func (server *Server) reviewBulk(writer http.ResponseWriter, request *http.Request) {
	summary, err := server.importer.GetReviewBulk(request.Context(), request.PathValue("bulkApprovalId"))
	if err != nil {
		writeReviewBulkError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", `"v`+strconv.FormatInt(summary.Version, 10)+`"`)
	writeJSON(writer, http.StatusOK, summary)
}

func (server *Server) reviewBulkItems(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	for key := range values {
		if key != "outcome" && key != "cursor" && key != "limit" {
			writeReviewBulkError(writer, request, libraryimport.ErrReviewBulkConflict)
			return
		}
		if len(values[key]) != 1 {
			writeReviewBulkError(writer, request, libraryimport.ErrReviewBulkConflict)
			return
		}
	}
	limit := 50
	if values.Get("limit") != "" {
		parsed, err := strconv.Atoi(values.Get("limit"))
		if err != nil || parsed < 1 || parsed > 50 {
			writeReviewBulkError(writer, request, libraryimport.ErrReviewBulkConflict)
			return
		}
		limit = parsed
	}
	page, err := server.importer.ListReviewBulkItems(
		request.Context(), request.PathValue("bulkApprovalId"), values.Get("outcome"), values.Get("cursor"), limit,
	)
	if err != nil {
		writeReviewBulkError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func (server *Server) cancelReviewBulk(writer http.ResponseWriter, request *http.Request) {
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
	if err := decodeJSON(writer, request, &body, 8<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "取消原因无效", map[string]any{})
		return
	}
	summary, err := server.importer.CancelReviewBulk(
		request.Context(), request.PathValue("bulkApprovalId"), version, body.Reason,
	)
	if err != nil {
		writeReviewBulkError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", `"v`+strconv.FormatInt(summary.Version, 10)+`"`)
	writeJSON(writer, http.StatusOK, summary)
}

func (server *Server) retryReviewBulk(writer http.ResponseWriter, request *http.Request) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct{}
	if err := decodeJSON(writer, request, &body, 1024); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "重试请求无效", map[string]any{})
		return
	}
	summary, err := server.importer.RetryReviewBulk(
		request.Context(), request.PathValue("bulkApprovalId"), version,
	)
	if err != nil {
		writeReviewBulkError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", `"v`+strconv.FormatInt(summary.Version, 10)+`"`)
	writeJSON(writer, http.StatusAccepted, summary)
}
