package httpapi

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"retrom/internal/authn"
)

const operationIDContextKey contextKey = "openapi-operation-id"

var domainIdempotencyOperations = map[string]struct{}{
	"deleteAdminAccountLink":         {},
	"deleteAdminUser":                {},
	"patchAdminUser":                 {},
	"postAdminInvitation":            {},
	"postAdminUserPasswordResetLink": {},
	"postLaunch":                     {},
	"postRuntimeSaveState":           {},
	"putRuntimePersistentSave":       {},
	"postAdminGameContentRevision":   {},
}

type bufferedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
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

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
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
		server.idempotency.Lock()
		defer server.idempotency.Unlock()
		now := server.now().UnixMilli()
		_, _ = server.database.ExecContext(
			request.Context(),
			`
DELETE
FROM idempotency_records
WHERE operation_id=?
AND key=?
AND principal_id=?
AND expires_at_ms<=?
`,
			operationID,
			key,
			principalID,
			now,
		)
		var storedDigest, headersJSON string
		var storedStatus int
		var storedBody []byte
		err = server.database.QueryRowContext(request.Context(), `
SELECT request_digest,
http_status,
response_headers_json,
response_body
FROM idempotency_records
WHERE operation_id=?
AND key=?
AND principal_id=?
`, operationID, key, principalID).
			Scan(&storedDigest, &storedStatus, &headersJSON, &storedBody)
		if err == nil {
			if storedDigest != digest {
				writeError(
					writer,
					request,
					http.StatusConflict,
					"IDEMPOTENCY_KEY_REUSED",
					"幂等键已用于另一请求",
					map[string]any{},
				)
				return
			}
			var headers map[string]string
			_ = json.Unmarshal([]byte(headersJSON), &headers)
			for name, value := range headers {
				writer.Header().Set(name, value)
			}
			writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
			writer.WriteHeader(storedStatus)
			_, _ = writer.Write(storedBody)
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			server.databaseError(writer, request, err)
			return
		}
		response := &bufferedResponse{header: make(http.Header)}
		next.ServeHTTP(response, request)
		if response.status == 0 {
			response.status = http.StatusOK
		}
		if response.status >= 200 && response.status < 300 && response.body.Len() <= 1<<20 {
			headers := responseHeadersForReplay(response.header)
			encodedHeaders, _ := json.Marshal(headers)
			responseBody := make([]byte, response.body.Len())
			copy(responseBody, response.body.Bytes())
			_, err = server.database.ExecContext(
				request.Context(),
				`
INSERT INTO idempotency_records(principal_id,
operation_id,
key,
request_digest,
http_status,
response_headers_json,
response_body,
created_at_ms,
expires_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
				principalID,
				operationID,
				key,
				digest,
				response.status,
				string(encodedHeaders),
				responseBody,
				now,
				now+int64(24*time.Hour/time.Millisecond),
			)
			if err != nil {
				server.databaseError(writer, request, err)
				return
			}
		}
		copyResponse(writer, response)
	})
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
