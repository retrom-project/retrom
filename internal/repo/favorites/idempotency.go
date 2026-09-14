package favorites

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/model/favorites"
)

func (records idempotencyRecords) Find(
	ctx context.Context, key favorites.IdempotencyKey, now int64,
) (favorites.IdempotencyRecord, bool, error) {
	if _, err := records.database.ExecContext(ctx, `DELETE FROM idempotency_records
WHERE principal_id=? AND operation_id=? AND key=? AND expires_at_ms<=?`,
		key.PrincipalID, key.Operation, key.Key, now); err != nil {
		return favorites.IdempotencyRecord{}, false, fmt.Errorf("prune favorite idempotency: %w", err)
	}
	var record favorites.IdempotencyRecord
	var headers string
	err := records.database.QueryRowContext(ctx, `
SELECT request_digest,http_status,response_headers_json,response_body,created_at_ms,expires_at_ms
FROM idempotency_records WHERE principal_id=? AND operation_id=? AND key=?`, key.PrincipalID, key.Operation, key.Key).
		Scan(&record.Digest, &record.Response.Status, &headers,
			&record.Response.Body, &record.CreatedAtMS, &record.ExpiresAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return favorites.IdempotencyRecord{}, false, nil
	}
	if err != nil {
		return favorites.IdempotencyRecord{}, false, fmt.Errorf("read favorite idempotency: %w", err)
	}
	if err := json.Unmarshal([]byte(headers), &record.Response.Headers); err != nil {
		return favorites.IdempotencyRecord{}, false, fmt.Errorf("decode favorite idempotency headers: %w", err)
	}
	return record, true, nil
}

func (records idempotencyRecords) Save(
	ctx context.Context, key favorites.IdempotencyKey, record favorites.IdempotencyRecord,
) error {
	headers, err := json.Marshal(record.Response.Headers)
	if err != nil {
		return fmt.Errorf("encode favorite idempotency headers: %w", err)
	}
	_, err = records.database.ExecContext(ctx, `INSERT INTO idempotency_records(
principal_id,operation_id,key,request_digest,http_status,response_headers_json,response_body,created_at_ms,expires_at_ms
) VALUES(?,?,?,?,?,?,?,?,?)`, key.PrincipalID, key.Operation, key.Key, record.Digest,
		record.Response.Status, string(headers), record.Response.Body, record.CreatedAtMS, record.ExpiresAtMS)
	if err != nil {
		return fmt.Errorf("save favorite idempotency: %w", err)
	}
	return nil
}
