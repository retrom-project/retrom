package httpapi

import (
	"errors"
	"net/http"

	"retrom/internal/libraryimport"
)

func (server *Server) deduplicateReviews(writer http.ResponseWriter, request *http.Request) {
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body libraryimport.ReviewDeduplicateRequest
	if err := decodeJSON(writer, request, &body, 16<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "去重请求无效", map[string]any{})
		return
	}
	result, err := server.importer.DeduplicateReviews(request.Context(), body)
	if errors.Is(err, libraryimport.ErrReviewBulkInvalidScope) {
		writeError(writer, request, http.StatusBadRequest, "REVIEW_BULK_INVALID_SCOPE", "审核筛选范围或去重游标无效", map[string]any{})
		return
	}
	if err != nil {
		writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "本页去重未完成，请重试", map[string]any{})
		return
	}
	writeJSON(writer, http.StatusOK, result)
}
