package httpapi

import (
	"errors"
	"net/http"

	libraryimportmodel "retrom/internal/model/libraryimport"
)

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
	result, err := server.reviewDiscards.Discard(request.Context(), libraryimportmodel.ReviewDiscardRequest{
		ItemID: request.PathValue("importItemId"), ExpectedVersion: version,
		Reason: reason, Mode: libraryimportmodel.ReviewDiscardSingle,
	})
	if err != nil && !errors.Is(err, libraryimportmodel.ErrInvalid) {
		server.databaseError(writer, request, err)
		return
	}
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusConflict,
			"REVIEW_DECISION_CONFLICT",
			"审核状态或版本已经变化",
			map[string]any{},
		)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}
