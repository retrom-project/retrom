package httpapi

import (
	"errors"
	"net/http"

	application "retrom/internal/model/libraryimport"
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
		Reason              *string  `json:"reason"`
		DuplicatePolicy     string   `json:"duplicatePolicy"`
		AcknowledgedGameIDs []string `json:"acknowledgedGameIds"`
	}
	if err := decodeJSON(writer, request, &body, 8<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "审核决定无效", map[string]any{})
		return
	}
	approved, err := server.reviewApprovals.Approve(request.Context(), application.ReviewApprovalRequest{
		ItemID: request.PathValue("importItemId"), ExpectedVersion: version,
		Decision: application.ReviewApprovalDecision{
			Reason:              body.Reason,
			DuplicatePolicy:     body.DuplicatePolicy,
			AcknowledgedGameIDs: body.AcknowledgedGameIDs,
		},
	})
	if err != nil {
		var duplicateConflict *application.DuplicateConflict
		if errors.As(err, &duplicateConflict) {
			writeError(
				writer,
				request,
				http.StatusConflict,
				"DUPLICATE_GAME_CONFIRMATION_REQUIRED",
				"相同游戏文件已关联到已发布游戏；继续发布可能产生重复游戏",
				map[string]any{
					"contentIdentityDigest": duplicateConflict.ContentIdentityDigest,
					"games":                 duplicateConflict.Games,
				},
			)
			return
		}
		if errors.Is(err, application.ErrInvalid) {
			writeError(writer, request, http.StatusConflict, "REVIEW_VALIDATION_STALE", "审核输入或验证结果已经变化", map[string]any{})
			return
		}
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, approved)
}
