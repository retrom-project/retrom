package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"retrom/internal/authn"
	"retrom/internal/payloadrelease"
)

const deleteGameOperation = "deleteAdminGame"

type deleteGameRequest struct {
	ConfirmTitle string `json:"confirmTitle"`
	ImpactDigest string `json:"impactDigest"`
}

type deleteGameResponse struct {
	GameID              string  `json:"gameId"`
	Status              string  `json:"status"`
	PayloadState        string  `json:"payloadState"`
	PayloadReleaseJobID *string `json:"payloadReleaseJobId"`
}

type storedIdempotentResponse struct {
	digest, headers string
	status          int
	body            []byte
}

type deleteGameInput struct {
	expected      int64
	body          deleteGameRequest
	principal     authn.Principal
	requestDigest string
}

type deleteGameState struct {
	title, status, payloadState string
	releaseJobID                sql.NullString
	version                     int64
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

func loadDeleteGameState(
	ctx context.Context,
	transaction *sql.Tx,
	gameID string,
) (deleteGameState, error) {
	var state deleteGameState
	err := transaction.QueryRowContext(ctx, `
SELECT m.title,g.status,g.payload_state,g.payload_release_job_id,g.version
FROM games g
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
WHERE g.id=?
`, gameID).Scan(
		&state.title, &state.status, &state.payloadState, &state.releaseJobID, &state.version,
	)
	if err != nil {
		return deleteGameState{}, fmt.Errorf("delete game state: %w", err)
	}
	return state, nil
}

func (server *Server) validateDeleteGameImpact(
	writer http.ResponseWriter,
	request *http.Request,
	transaction *sql.Tx,
	input deleteGameInput,
	state deleteGameState,
) (payloadrelease.GameImpact, bool) {
	if state.version != input.expected {
		writeError(writer, request, http.StatusConflict,
			"VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return payloadrelease.GameImpact{}, false
	}
	if input.body.ConfirmTitle != state.title {
		writeError(writer, request, http.StatusUnprocessableEntity,
			"GAME_DELETE_CONFIRMATION_MISMATCH", "确认标题不匹配", map[string]any{})
		return payloadrelease.GameImpact{}, false
	}
	impact, err := payloadrelease.GameDeleteImpactTx(
		request.Context(), transaction, request.PathValue("gameId"),
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return payloadrelease.GameImpact{}, false
	}
	if input.body.ImpactDigest != impact.ImpactDigest {
		writeError(writer, request, http.StatusConflict,
			"GAME_DELETE_IMPACT_STALE", "删除影响已经变化，请刷新后重试", map[string]any{})
		return payloadrelease.GameImpact{}, false
	}
	return impact, true
}

func loadDeleteGameReplay(
	ctx context.Context,
	transaction *sql.Tx,
	principalID, key string,
	now int64,
) (storedIdempotentResponse, bool, error) {
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM idempotency_records
WHERE principal_id=? AND operation_id=? AND key=? AND expires_at_ms<=?
`, principalID, deleteGameOperation, key, now); err != nil {
		return storedIdempotentResponse{}, false, fmt.Errorf("delete game idempotency expiry: %w", err)
	}
	var stored storedIdempotentResponse
	err := transaction.QueryRowContext(ctx, `
SELECT request_digest,http_status,response_headers_json,response_body
FROM idempotency_records WHERE principal_id=? AND operation_id=? AND key=?
`, principalID, deleteGameOperation, key).Scan(&stored.digest, &stored.status, &stored.headers, &stored.body)
	if errors.Is(err, sql.ErrNoRows) {
		return storedIdempotentResponse{}, false, nil
	}
	if err != nil {
		return storedIdempotentResponse{}, false, fmt.Errorf("delete game idempotency read: %w", err)
	}
	return stored, true, nil
}

func (server *Server) replayDeleteGameIfPresent(
	writer http.ResponseWriter,
	request *http.Request,
	transaction *sql.Tx,
	input deleteGameInput,
	now int64,
) bool {
	stored, found, err := loadDeleteGameReplay(
		request.Context(), transaction, input.principal.UserID,
		request.Header.Get("Idempotency-Key"), now,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return true
	}
	if !found {
		return false
	}
	server.replayIdempotentResponse(
		writer, request, deleteGameOperation, input.requestDigest, stored.digest,
		stored.status, stored.headers, stored.body,
	)
	return true
}

func (server *Server) respondToExistingGameTombstone(
	writer http.ResponseWriter,
	request *http.Request,
	transaction *sql.Tx,
	input deleteGameInput,
	state deleteGameState,
	now int64,
) bool {
	if state.status == "PUBLISHED" {
		return false
	}
	var releaseJob *string
	if state.releaseJobID.Valid {
		releaseJob = &state.releaseJobID.String
	}
	response := deleteGameResponse{
		GameID: request.PathValue("gameId"), Status: state.status,
		PayloadState: state.payloadState, PayloadReleaseJobID: releaseJob,
	}
	etag := fmt.Sprintf(`"v%d"`, state.version)
	responseBody, err := storeDeleteGameResponse(
		request.Context(), transaction, input.principal.UserID,
		request.Header.Get("Idempotency-Key"), input.requestDigest,
		etag, http.StatusOK, response, now,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return true
	}
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return true
	}
	writeStoredJSON(writer, http.StatusOK, etag, responseBody)
	return true
}

func storeDeleteGameResponse(
	ctx context.Context,
	transaction *sql.Tx,
	principalID, key, digest, etag string,
	status int,
	value deleteGameResponse,
	now int64,
) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("delete game response: %w", err)
	}
	body = append(body, '\n')
	headers, _ := json.Marshal(map[string]string{
		"Cache-Control": "private, no-store", "Content-Type": "application/json; charset=utf-8", "ETag": etag,
	})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO idempotency_records(principal_id,operation_id,key,request_digest,http_status,
response_headers_json,response_body,created_at_ms,expires_at_ms)
VALUES(?,?,?,?,?,?,?,?,?)
`, principalID, deleteGameOperation, key, digest, status, string(headers), body, now,
		now+int64(24*time.Hour/time.Millisecond)); err != nil {
		return nil, fmt.Errorf("delete game idempotency write: %w", err)
	}
	return body, nil
}

func writeStoredJSON(writer http.ResponseWriter, status int, etag string, body []byte) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("ETag", etag)
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func transitionDeletedGameRuntime(ctx context.Context, transaction *sql.Tx, gameID string, now int64) error {
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,CASE state WHEN 'QUEUED' THEN 'CANCELLED' ELSE 'CANCEL_REQUESTED' END,
'{"schemaVersion":1,"reason":"GAME_DELETED"}',?
FROM jobs WHERE scope_type='GAME' AND scope_id=?
AND kind IN ('GAME_FILE_REVISION','METADATA_SCRAPE','MEDIA_FETCH') AND state IN ('QUEUED','RUNNING')
`, now, gameID); err != nil {
		return fmt.Errorf("delete game mutation events: %w", err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{
			`UPDATE jobs SET state=CASE WHEN state='QUEUED' THEN 'CANCELLED' ELSE 'CANCEL_REQUESTED' END,
cancel_requested_at_ms=?,cancel_reason='game deleted',finished_at_ms=CASE WHEN state='QUEUED' THEN ? ELSE NULL END,
version=version+1,updated_at_ms=? WHERE scope_type='GAME' AND scope_id=?
AND kind IN ('GAME_FILE_REVISION','METADATA_SCRAPE','MEDIA_FETCH') AND state IN ('QUEUED','RUNNING')`,
			[]any{now, now, now, gameID},
		},
		{`UPDATE launch_sessions SET state='REVOKED',finished_at_ms=COALESCE(finished_at_ms,?),updated_at_ms=?,
version=version+1 WHERE game_id=? AND state IN ('CREATED','ACTIVE')`, []any{now, now, gameID}},
		{`UPDATE play_sessions SET state='ABANDONED',ended_at_ms=?,updated_at_ms=?,version=version+1
WHERE game_id=? AND state='ACTIVE'`, []any{now, now, gameID}},
		{`UPDATE netplay_sessions SET state='FAILED',finished_at_ms=?,end_reason='GAME_DELETED',updated_at_ms=?,
version=version+1 WHERE game_id=? AND state NOT IN ('FINISHED','FAILED')`, []any{now, now, gameID}},
		{
			`UPDATE netplay_rooms SET state='ENDED',ended_at_ms=?,end_reason='GAME_DELETED',updated_at_ms=?,
version=version+1 WHERE selected_game_id=? AND state IN ('DRAFT','WAITING','STARTING','RUNNING')`,
			[]any{now, now, gameID},
		},
		{`UPDATE launch_sessions SET save_state_id=NULL WHERE game_id=? AND save_state_id IS NOT NULL`, []any{gameID}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return fmt.Errorf("delete game runtime: %w", err)
		}
	}
	return nil
}
