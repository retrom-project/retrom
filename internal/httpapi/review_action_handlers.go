package httpapi

import (
	"errors"
	"net/http"

	"retrom/internal/libraryimport"
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
	approved, err := server.importer.ApproveWithDecision(
		request.Context(),
		request.PathValue("importItemId"),
		version,
		libraryimport.ApprovalDecision{
			Reason:              body.Reason,
			DuplicatePolicy:     body.DuplicatePolicy,
			AcknowledgedGameIDs: body.AcknowledgedGameIDs,
		},
	)
	if err != nil {
		var duplicateConflict *libraryimport.DuplicateConflict
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
		writeError(writer, request, http.StatusConflict, "REVIEW_VALIDATION_STALE", "审核输入或验证结果已经变化", map[string]any{})
		return
	}
	writeJSON(writer, http.StatusCreated, approved)
}
