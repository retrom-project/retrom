package httpapi

import (
	"errors"
	"net/http"

	libraryservice "retrom/internal/service/libraryimport"
)

func (server *Server) approveReview(writer http.ResponseWriter, request *http.Request) {
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
	if err := decodeJSON(writer, request, &body, 8<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "审核决定无效", map[string]any{})
		return
	}
	approved, err := server.reviewDeps.Approvals.Approve(request.Context(), libraryservice.ReviewApprovalRequest{
		ItemID: request.PathValue("importItemId"), ExpectedVersion: version,
		Decision: libraryservice.ReviewApprovalDecision{
			Reason: body.Reason,
		},
	})
	if err != nil {
		if errors.Is(err, libraryservice.ErrInvalid) {
			writeError(writer, request, http.StatusConflict, "REVIEW_VALIDATION_STALE", "审核输入或验证结果已经变化", map[string]any{})
			return
		}
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, approved)
}
