package platforminstance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/service/platforminstance"
)

func (writer records) Find(
	ctx context.Context, key platforminstance.IdempotencyKey, now int64,
) (platforminstance.IdempotencyRecord, bool, error) {
	if _, err := writer.database.ExecContext(ctx, `
DELETE FROM idempotency_records WHERE principal_id=? AND operation_id=? AND key=? AND expires_at_ms<=?
`, key.PrincipalID, key.Operation, key.Key, now); err != nil {
		return platforminstance.IdempotencyRecord{}, false, fmt.Errorf("platforminstance: prune idempotency: %w", err)
	}
	var record platforminstance.IdempotencyRecord
	var headers string
	err := writer.database.QueryRowContext(ctx, `
SELECT request_digest,http_status,response_headers_json,response_body,created_at_ms,expires_at_ms
FROM idempotency_records WHERE principal_id=? AND operation_id=? AND key=?
`, key.PrincipalID, key.Operation, key.Key).Scan(&record.Digest, &record.Response.Status, &headers,
		&record.Response.Body, &record.CreatedAtMS, &record.ExpiresAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return platforminstance.IdempotencyRecord{}, false, nil
	}
	if err != nil {
		return platforminstance.IdempotencyRecord{}, false, fmt.Errorf("platforminstance: read idempotency: %w", err)
	}
	record.Response.Headers = make(map[string]string)
	if err := json.Unmarshal([]byte(headers), &record.Response.Headers); err != nil {
		return platforminstance.IdempotencyRecord{}, false, fmt.Errorf("platforminstance: decode headers: %w", err)
	}
	return record, true, nil
}

func (writer records) Save(
	ctx context.Context, key platforminstance.IdempotencyKey, record platforminstance.IdempotencyRecord,
) error {
	headers, err := json.Marshal(record.Response.Headers)
	if err != nil {
		return fmt.Errorf("platforminstance: encode idempotency headers: %w", err)
	}
	_, err = writer.database.ExecContext(ctx, `
INSERT INTO idempotency_records(
 principal_id,operation_id,key,request_digest,http_status,response_headers_json,response_body,
 created_at_ms,expires_at_ms
) VALUES(?,?,?,?,?,?,?,?,?)
`, key.PrincipalID, key.Operation, key.Key, record.Digest, record.Response.Status, string(headers),
		record.Response.Body, record.CreatedAtMS, record.ExpiresAtMS)
	if err != nil {
		return fmt.Errorf("platforminstance: store idempotency: %w", err)
	}
	return nil
}
