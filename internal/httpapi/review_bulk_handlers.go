package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"retrom/internal/libraryimport"
)

func writeReviewBulkError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, libraryimport.ErrReviewBulkActive):
		writeError(writer, request, http.StatusConflict, "REVIEW_BULK_APPROVAL_ACTIVE", "已有快速审批正在执行", map[string]any{})
	case errors.Is(err, libraryimport.ErrReviewBulkTooLarge):
		writeError(writer, request, http.StatusUnprocessableEntity, "REVIEW_BULK_SCOPE_TOO_LARGE",
			"待审核条目超过 10000 个", map[string]any{})
	case errors.Is(err, libraryimport.ErrReviewBulkEmpty):
		writeError(writer, request, http.StatusConflict, "REVIEW_BULK_SCOPE_EMPTY", "当前没有待审核条目", map[string]any{})
	case errors.Is(err, libraryimport.ErrReviewBulkConflict):
		writeError(writer, request, http.StatusConflict, "REVIEW_BULK_VERSION_CONFLICT", "快速审批状态已经变化", map[string]any{})
	case errors.Is(err, sql.ErrNoRows):
		writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "资源不存在", map[string]any{})
	default:
		writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "快速审批暂时不可用", map[string]any{})
	}
}

func serverNotFound(writer http.ResponseWriter, request *http.Request) {
	writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "资源不存在", map[string]any{})
}

func (server *Server) createReviewBulk(writer http.ResponseWriter, request *http.Request) {
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct{}
	if err := decodeJSON(writer, request, &body, 1024); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "快速审批请求无效", map[string]any{})
		return
	}
	created, err := server.importer.CreateReviewBulk(request.Context())
	if err != nil {
		writeReviewBulkError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", `"v1"`)
	writeJSON(writer, http.StatusAccepted, created)
}

func (server *Server) activeReviewBulk(writer http.ResponseWriter, request *http.Request) {
	active, found, err := server.importer.GetActiveReviewBulk(request.Context())
	if err != nil {
		writeReviewBulkError(writer, request, err)
		return
	}
	if !found {
		writeJSON(writer, http.StatusOK, map[string]any{"activeBulkApproval": nil})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"activeBulkApproval": active})
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
