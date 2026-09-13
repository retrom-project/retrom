package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"retrom/internal/capability/security/authn"
)

const deleteGameOperation = "deleteAdminGame"

type deleteGameRequest struct {
	ConfirmTitle string `json:"confirmTitle"`
	ImpactDigest string `json:"impactDigest"`
}

type deleteGameInput struct {
	expected      int64
	body          deleteGameRequest
	principal     authn.Principal
	requestDigest string
}

func parseDeleteGameInput(
	writer http.ResponseWriter,
	request *http.Request,
) (deleteGameInput, bool) {
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest,
			"INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return deleteGameInput{}, false
	}
	expected, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(writer, request, http.StatusPreconditionRequired,
			"PRECONDITION_REQUIRED", "需要当前资源版本", map[string]any{})
		return deleteGameInput{}, false
	}
	var body deleteGameRequest
	if err := decodeJSON(writer, request, &body, 4096); err != nil ||
		len(body.ImpactDigest) != 64 || body.ImpactDigest != strings.ToLower(body.ImpactDigest) {
		writeError(writer, request, http.StatusBadRequest,
			"INVALID_REQUEST", "删除确认无效", map[string]any{})
		return deleteGameInput{}, false
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	encodedRequest, _ := json.Marshal(body)
	requestDigest, valid := semanticRequestDigest(
		request, principal.UserID, deleteGameOperation, encodedRequest,
	)
	if !valid {
		writeError(writer, request, http.StatusBadRequest,
			"INVALID_REQUEST", "删除确认无效", map[string]any{})
		return deleteGameInput{}, false
	}
	return deleteGameInput{
		expected: expected, body: body, principal: principal, requestDigest: requestDigest,
	}, true
}

func writeStoredJSON(writer http.ResponseWriter, status int, etag string, body []byte) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("ETag", etag)
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}
