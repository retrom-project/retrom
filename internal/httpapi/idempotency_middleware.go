package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"retrom/internal/telemetry"

	"retrom/internal/authn"
	idempotencyservice "retrom/internal/service/idempotency"
)

const operationIDContextKey contextKey = "openapi-operation-id"

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

func (server *Server) idempotencyHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		operation, _ := request.Context().Value(operationIDContextKey).(string)
		operation = lowerFirst(operation)
		key := request.Header.Get("Idempotency-Key")
		if _, supported := commandResponses[operation]; !supported || key == "" {
			next.ServeHTTP(writer, request)
			return
		}
		identity, valid := commandIdentity(writer, request, operation, key)
		if !valid {
			return
		}
		ctx, command := idempotencyservice.NewCommand(request.Context(), identity,
			server.commandEncoder(operation), server.now().UnixMilli())
		var response *bufferedResponse
		var stored idempotencyservice.Receipt
		var replay bool
		err := server.systemDeps.Idempotency.Coordinate(ctx, identity, command, func(ctx context.Context) error {
			var err error
			stored, replay, err = server.loadCommandReceipt(ctx, identity)
			if err != nil || replay {
				return err
			}
			response = &bufferedResponse{header: make(http.Header)}
			started := time.Now()
			next.ServeHTTP(response, request.WithContext(ctx))
			telemetry.RecordTiming(ctx, telemetry.Handler, time.Since(started))
			if response.status == 0 {
				response.status = http.StatusOK
			}
			if err := command.Failure(); err != nil {
				return fmt.Errorf("complete command transaction: %w", err)
			}
			if receipt, committed := command.Receipt(); committed {
				stored = receipt
				return nil
			}
			if response.status >= 200 && response.status < 300 {
				return idempotencyservice.ErrCommandIncomplete
			}
			return nil
		})
		if err != nil {
			server.writeCommandError(writer, request, err)
			return
		}
		server.writeCommandResponse(ctx, writer, request, identity, response, stored, replay)
	})
}

func commandIdentity(writer http.ResponseWriter, request *http.Request, operation, key string) (
	idempotencyservice.Request, bool,
) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	principalID := principal.UserID
	if principalID == "" {
		principalID = "SYSTEM"
	}
	contents, err := io.ReadAll(io.LimitReader(request.Body, (16<<20)+1))
	if err != nil || len(contents) > 16<<20 {
		writeError(writer, request, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "请求内容超过限制", map[string]any{})
		return idempotencyservice.Request{}, false
	}
	request.Body = io.NopCloser(bytes.NewReader(contents))
	digest, ok := semanticRequestDigest(request, principalID, operation, contents)
	if !ok {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "幂等请求内容无效", map[string]any{})
		return idempotencyservice.Request{}, false
	}
	return idempotencyservice.Request{PrincipalID: principalID, OperationID: operation, Key: key, Digest: digest}, true
}

func (server *Server) loadCommandReceipt(ctx context.Context, identity idempotencyservice.Request) (
	idempotencyservice.Receipt, bool, error,
) {
	started := time.Now()
	defer func() { telemetry.RecordTiming(ctx, telemetry.ReceiptIO, time.Since(started)) }()
	records := server.systemDeps.Idempotency
	if err := records.PurgeExpired(ctx, identity.OperationID, identity.Key, identity.PrincipalID,
		server.now().UnixMilli()); err != nil {
		return idempotencyservice.Receipt{}, false, fmt.Errorf("purge command receipt: %w", err)
	}
	receipt, found, err := records.Lookup(ctx, identity.OperationID, identity.Key, identity.PrincipalID)
	if err != nil {
		return idempotencyservice.Receipt{}, false, fmt.Errorf("read command receipt: %w", err)
	}
	return receipt, found, nil
}

func (server *Server) writeCommandResponse(ctx context.Context, writer http.ResponseWriter, request *http.Request,
	identity idempotencyservice.Request, response *bufferedResponse,
	stored idempotencyservice.Receipt, replay bool,
) {
	if replay {
		if identity.OperationID == "postAdminReviewPreview" && !server.ensureRuntimeSession(writer, request) {
			return
		}
		server.replayIdempotentResponse(writer, request, identity.Digest, stored.RequestDigest,
			stored.HTTPStatus, stored.HeadersJSON, stored.Body)
		return
	}
	if stored.HTTPStatus == 0 {
		copyResponse(writer, response)
		return
	}
	// The committed transaction owns the response even if a later read fails or
	// a worker advances the resource. Identity coordination has already ended.
	applyCommandReceipt(response, stored)
	copyResponse(writer, response)
	started := time.Now()
	response.runAfterCommit(true)
	telemetry.RecordTiming(ctx, telemetry.AfterCommit, time.Since(started))
}

func applyCommandReceipt(response *bufferedResponse, receipt idempotencyservice.Receipt) {
	var headers map[string]string
	_ = json.Unmarshal([]byte(receipt.HeadersJSON), &headers)
	for name, value := range headers {
		response.header.Set(name, value)
	}
	response.status = receipt.HTTPStatus
	response.body.Reset()
	_, _ = response.body.Write(receipt.Body)
}

func (server *Server) replayIdempotentResponse(
	writer http.ResponseWriter,
	request *http.Request,
	digest, storedDigest string,
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

func (server *Server) writeCommandError(writer http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, idempotencyservice.ErrKeyReused) {
		writeError(writer, request, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "幂等键已用于另一请求", map[string]any{})
		return
	}
	server.databaseError(writer, request, err)
}
