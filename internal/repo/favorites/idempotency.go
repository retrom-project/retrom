package favorites

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/model/favorites"
	"retrom/internal/repo/dbexec"
)

type idempotencyRecord struct {
	digest                   string
	response                 favorites.IdempotentResponse
	createdAtMS, expiresAtMS int64
}

func findIdempotencyRecord(
	ctx context.Context, database dbexec.Executor, envelope favorites.IdempotencyEnvelope,
) (idempotencyRecord, bool, error) {
	if _, err := database.ExecContext(ctx, `DELETE FROM idempotency_records
WHERE principal_id=? AND operation_id=? AND key=? AND expires_at_ms<=?`,
		envelope.PrincipalID, envelope.Operation, envelope.Key, envelope.NowMS); err != nil {
		return idempotencyRecord{}, false, fmt.Errorf("prune favorite idempotency: %w", err)
	}
	var record idempotencyRecord
	var headers string
	err := database.QueryRowContext(ctx, `
SELECT request_digest,http_status,response_headers_json,response_body,created_at_ms,expires_at_ms
FROM idempotency_records WHERE principal_id=? AND operation_id=? AND key=?`,
		envelope.PrincipalID, envelope.Operation, envelope.Key).
		Scan(&record.digest, &record.response.Status, &headers,
			&record.response.Body, &record.createdAtMS, &record.expiresAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return idempotencyRecord{}, false, nil
	}
	if err != nil {
		return idempotencyRecord{}, false, fmt.Errorf("read favorite idempotency: %w", err)
	}
	if err := json.Unmarshal([]byte(headers), &record.response.Headers); err != nil {
		return idempotencyRecord{}, false, fmt.Errorf("decode favorite idempotency headers: %w", err)
	}
	return record, true, nil
}

func saveIdempotencyRecord(
	ctx context.Context, database dbexec.Executor,
	envelope favorites.IdempotencyEnvelope, response favorites.IdempotentResponse,
) error {
	headers, err := json.Marshal(response.Headers)
	if err != nil {
		return fmt.Errorf("encode favorite idempotency headers: %w", err)
	}
	_, err = database.ExecContext(ctx, `INSERT INTO idempotency_records(
principal_id,operation_id,key,request_digest,http_status,response_headers_json,response_body,created_at_ms,expires_at_ms
) VALUES(?,?,?,?,?,?,?,?,?)`, envelope.PrincipalID, envelope.Operation, envelope.Key, envelope.Digest,
		response.Status, string(headers), response.Body, envelope.NowMS, envelope.ExpiresAtMS)
	if err != nil {
		return fmt.Errorf("save favorite idempotency: %w", err)
	}
	return nil
}

// checkIdempotency returns a stored response if an idempotent key was already used.
// Returns (response, true, nil) if replayed, (zero, false, nil) if fresh, or error.
func checkIdempotency(
	ctx context.Context, database dbexec.Executor, envelope favorites.IdempotencyEnvelope,
) (favorites.IdempotentResponse, bool, error) {
	stored, found, err := findIdempotencyRecord(ctx, database, envelope)
	if err != nil {
		return favorites.IdempotentResponse{}, false, err
	}
	if !found {
		return favorites.IdempotentResponse{}, false, nil
	}
	if stored.digest != envelope.Digest {
		return favorites.IdempotentResponse{}, false, favorites.ErrIdempotencyReused
	}
	stored.response.Replayed = true
	return stored.response, true, nil
}
