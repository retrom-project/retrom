package platforminstance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/model/platforminstance"
)

type idempotencyRecord struct {
	digest   string
	response platforminstance.IdempotentResponse
}

func (writer records) findIdempotency(
	ctx context.Context, key platforminstance.IdempotencyKey, now int64,
) (idempotencyRecord, bool, error) {
	if _, err := writer.database.ExecContext(ctx, `
DELETE FROM idempotency_records WHERE principal_id=? AND operation_id=? AND key=? AND expires_at_ms<=?
`, key.PrincipalID, key.Operation, key.Key, now); err != nil {
		return idempotencyRecord{}, false, fmt.Errorf("platforminstance: prune idempotency: %w", err)
	}
	var record idempotencyRecord
	var headers string
	var createdAtMS, expiresAtMS int64
	err := writer.database.QueryRowContext(ctx, `
SELECT request_digest,http_status,response_headers_json,response_body,created_at_ms,expires_at_ms
FROM idempotency_records WHERE principal_id=? AND operation_id=? AND key=?
`, key.PrincipalID, key.Operation, key.Key).Scan(&record.digest, &record.response.Status, &headers,
		&record.response.Body, &createdAtMS, &expiresAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return idempotencyRecord{}, false, nil
	}
	if err != nil {
		return idempotencyRecord{}, false, fmt.Errorf("platforminstance: read idempotency: %w", err)
	}
	record.response.Headers = make(map[string]string)
	if err := json.Unmarshal([]byte(headers), &record.response.Headers); err != nil {
		return idempotencyRecord{}, false, fmt.Errorf("platforminstance: decode headers: %w", err)
	}
	return record, true, nil
}

func (writer records) saveIdempotency(
	ctx context.Context, key platforminstance.IdempotencyKey, digest string,
	response platforminstance.IdempotentResponse, nowMS, expiresAtMS int64,
) error {
	headers, err := json.Marshal(response.Headers)
	if err != nil {
		return fmt.Errorf("platforminstance: encode idempotency headers: %w", err)
	}
	_, err = writer.database.ExecContext(ctx, `
INSERT INTO idempotency_records(
 principal_id,operation_id,key,request_digest,http_status,response_headers_json,response_body,
 created_at_ms,expires_at_ms
) VALUES(?,?,?,?,?,?,?,?,?)
`, key.PrincipalID, key.Operation, key.Key, digest, response.Status, string(headers),
		response.Body, nowMS, expiresAtMS)
	if err != nil {
		return fmt.Errorf("platforminstance: store idempotency: %w", err)
	}
	return nil
}
