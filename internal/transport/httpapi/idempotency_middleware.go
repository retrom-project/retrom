package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"retrom/internal/capability/security/authn"
	idempotencyservice "retrom/internal/service/idempotency"
)

const operationIDContextKey contextKey = "openapi-operation-id"

var domainIdempotencyOperations = map[string]struct{}{
	"deleteAdminAccountLink":                        {},
	"deleteAdminUser":                               {},
	"patchAdminUser":                                {},
	"postAdminInvitation":                           {},
	"postAdminUserPasswordResetLink":                {},
	"postLaunch":                                    {},
	"postLocalGameSave":                             {},
	"postRuntimeSaveState":                          {},
	"postAdminGameContentReplacement":               {},
	"postAdminPlatformInstanceRecommendationsApply": {},
	"postFavoriteOrganize":                          {},
	"postFavoriteUnfavorite":                        {},
	"postFavoriteRestore":                           {},
	"postFavoriteFolder":                            {},
	"patchFavoriteFolder":                           {},
	"deleteFavoriteFolder":                          {},
	"deleteAdminGame":                               {},
}

type bufferedResponse struct {
	header      http.Header
	body        bytes.Buffer
	status      int
	afterCommit []func()
}

func (response *bufferedResponse) Header() http.Header { return response.header }

func (response *bufferedResponse) WriteHeader(status int) {
	if response.status == 0 {
		response.status = status
	}
}

func (response *bufferedResponse) Write(contents []byte) (int, error) {
	if response.status == 0 {
		response.status = http.StatusOK
	}
	written, err := response.body.Write(contents)
	if err != nil {
		return written, fmt.Errorf("buffer idempotent response: %w", err)
	}
	return written, nil
}

// Contract branches stay contiguous for a single auditable decision.
func (server *Server) idempotencyHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		operationID, _ := request.Context().Value(operationIDContextKey).(string)
		operationID = lowerFirst(operationID)
		key := request.Header.Get("Idempotency-Key")
		if operationID == "" || key == "" {
			next.ServeHTTP(writer, request)
			return
		}
		if _, handledByDomain := domainIdempotencyOperations[operationID]; handledByDomain {
			next.ServeHTTP(writer, request)
			return
		}
		principal, _ := authn.PrincipalFromContext(request.Context())
		principalID := principal.UserID
		if principalID == "" {
			principalID = "SYSTEM"
		}
		contents, err := io.ReadAll(io.LimitReader(request.Body, (16<<20)+1))
		if err != nil || len(contents) > 16<<20 {
			writeError(
				writer,
				request,
				http.StatusRequestEntityTooLarge,
				"REQUEST_TOO_LARGE",
				"请求内容超过限制",
				map[string]any{},
			)
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(contents))
		digest, ok := semanticRequestDigest(request, principalID, operationID, contents)
		if !ok {
			next.ServeHTTP(writer, request)
			return
		}
		server.lockIdempotentRequest()
		defer server.idempotency.Unlock()
		idempotencyRecords := server.idempotencyRecords()
		now := server.now().UnixMilli()
		if err := idempotencyRecords.PurgeExpired(
			request.Context(), operationID, key, principalID, now,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		stored, found, err := idempotencyRecords.Lookup(
			request.Context(), operationID, key, principalID,
		)
		if err != nil {
			server.databaseError(writer, request, err)
			return
		}
		if found {
			server.replayIdempotentResponse(
				writer, request, operationID, digest, stored.RequestDigest,
				stored.HTTPStatus, stored.HeadersJSON, stored.Body,
			)
			return
		}
		response := &bufferedResponse{header: make(http.Header)}
		next.ServeHTTP(response, request)
		if response.status == 0 {
			response.status = http.StatusOK
		}
		committed, err := server.storeBufferedIdempotencyResponse(
			request.Context(), idempotencyRecords, operationID, key, principalID, digest, response, now,
		)
		if err != nil {
			server.databaseError(writer, request, err)
			return
		}
		copyResponse(writer, response)
		response.runAfterCommit(committed)
	})
}

func (server *Server) storeBufferedIdempotencyResponse(
	ctx context.Context,
	records *idempotencyservice.Service,
	operationID, key, principalID, digest string,
	response *bufferedResponse,
	nowMS int64,
) (bool, error) {
	if response.status < 200 || response.status >= 300 || response.body.Len() > 1<<20 {
		return false, nil
	}
	headers := responseHeadersForReplay(response.header)
	encodedHeaders, err := json.Marshal(headers)
	if err != nil {
		return false, fmt.Errorf("encode idempotency response headers: %w", err)
	}
	responseBody := make([]byte, response.body.Len())
	copy(responseBody, response.body.Bytes())
	if err := records.Store(
		ctx, operationID, key, principalID,
		idempotencyservice.Receipt{
			RequestDigest: digest,
			HTTPStatus:    response.status,
			HeadersJSON:   string(encodedHeaders),
			Body:          responseBody,
		},
		nowMS, nowMS+int64(24*time.Hour/time.Millisecond),
	); err != nil {
		return false, fmt.Errorf("store idempotency response: %w", err)
	}
	return true, nil
}

func (server *Server) lockIdempotentRequest() {
	server.idempotencyQueueMu.Lock()
	server.idempotencyQueueWaiters++
	server.idempotencyQueueMu.Unlock()

	server.idempotency.Lock()

	server.idempotencyQueueMu.Lock()
	server.idempotencyQueueWaiters--
	if server.idempotencyQueueWaiters == 0 {
		server.idempotencyQueueDrained.Broadcast()
	}
	server.idempotencyQueueMu.Unlock()
}

func (server *Server) waitForQueuedIdempotentRequests() {
	server.idempotencyQueueMu.Lock()
	defer server.idempotencyQueueMu.Unlock()
	for server.idempotencyQueueWaiters > 0 {
		server.idempotencyQueueDrained.Wait()
	}
}

func (server *Server) replayIdempotentResponse(
	writer http.ResponseWriter,
	request *http.Request,
	operationID, digest, storedDigest string,
	storedStatus int,
	headersJSON string,
	storedBody []byte,
) {
	if storedDigest != digest {
		writeError(
			writer, request, http.StatusConflict,
			"IDEMPOTENCY_KEY_REUSED", "幂等键已用于另一请求", map[string]any{},
		)
		return
	}
	var headers map[string]string
	_ = json.Unmarshal([]byte(headersJSON), &headers)
	if operationID == "postNetplayLaunch" {
		if err := server.reissueNetplayCookies(writer, request, storedBody); err != nil {
			serverError(writer, request, err)
			return
		}
	}
	for name, value := range headers {
		writer.Header().Set(name, value)
	}
	writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
	writer.WriteHeader(storedStatus)
	_, _ = writer.Write(storedBody)
}

func semanticRequestDigest(request *http.Request, principalID, operationID string, contents []byte) (string, bool) {
	var body any
	mediaType := ""
	if request.Header.Get("Content-Type") != "" {
		parsed, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil {
			return "", false
		}
		mediaType = parsed
	}
	if len(bytes.TrimSpace(contents)) > 0 {
		if mediaType != "application/json" || json.Unmarshal(contents, &body) != nil {
			return "", false
		}
	}
	canonical, err := json.Marshal(map[string]any{
		"body": body, "ifMatch": nullableHeader(request.Header.Get("If-Match")), "mediaType": mediaType,
		"operationId": operationID, "path": request.URL.EscapedPath(), "principalId": principalID,
		"query": request.URL.Query(),
	})
	if err != nil {
		return "", false
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), true
}

func nullableHeader(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func responseHeadersForReplay(header http.Header) map[string]string {
	result := make(map[string]string)
	for _, name := range []string{"Content-Type", "Location", "ETag", "Retry-After"} {
		if value := header.Get(name); value != "" {
			result[name] = value
		}
	}
	return result
}

func copyResponse(writer http.ResponseWriter, response *bufferedResponse) {
	for name, values := range response.header {
		for _, value := range values {
			writer.Header().Add(name, value)
		}
	}
	writer.WriteHeader(response.status)
	_, _ = writer.Write(response.body.Bytes())
}

func lowerFirst(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToLower(value[:1]) + value[1:]
}
