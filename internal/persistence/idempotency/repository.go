package idempotency

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"

	application "retrom/internal/service/idempotency"
)

type Repository struct {
	database dbapi.DB
}

func New(database dbapi.DB) *Repository {
	return &Repository{database: database}
}

func (repository *Repository) DeleteExpired(
	ctx context.Context, operationID, key, principalID string, nowMS int64,
) error {
	if _, err := repository.database.ExecContext(ctx, `
DELETE FROM idempotency_records
WHERE operation_id=?
AND key=?
AND principal_id=?
AND expires_at_ms<=?
`, operationID, key, principalID, nowMS); err != nil {
		return fmt.Errorf("delete expired idempotency receipt: %w", err)
	}
	return nil
}

func (repository *Repository) Find(
	ctx context.Context, operationID, key, principalID string,
) (application.Receipt, bool, error) {
	var receipt application.Receipt
	err := dbapi.QueryRowContext(ctx, repository.database, `
SELECT request_digest,http_status,response_headers_json,response_body
FROM idempotency_records
WHERE operation_id=?
AND key=?
AND principal_id=?
`, operationID, key, principalID).Scan(
		&receipt.RequestDigest, &receipt.HTTPStatus, &receipt.HeadersJSON, &receipt.Body,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.Receipt{}, false, nil
	}
	if err != nil {
		return application.Receipt{}, false, fmt.Errorf("read idempotency receipt: %w", err)
	}
	return receipt, true, nil
}

func save(ctx context.Context, executor dbapi.Executor, operationID, key, principalID string,
	receipt application.Receipt, createdAtMS, expiresAtMS int64,
) error {
	result, err := executor.ExecContext(ctx, `
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
?) ON CONFLICT(principal_id,operation_id,key) DO UPDATE
SET key=EXCLUDED.key WHERE idempotency_records.request_digest=EXCLUDED.request_digest
AND idempotency_records.http_status=EXCLUDED.http_status
AND idempotency_records.response_headers_json=EXCLUDED.response_headers_json
AND idempotency_records.response_body=EXCLUDED.response_body
`, principalID, operationID, key, receipt.RequestDigest, receipt.HTTPStatus,
		receipt.HeadersJSON, receipt.Body, createdAtMS, expiresAtMS)
	if err != nil {
		return fmt.Errorf("write idempotency receipt: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count command completion: %w", err)
	}
	if count == 1 {
		return nil
	}
	var digest string
	if err := dbapi.QueryRowContext(ctx, executor, `SELECT request_digest FROM idempotency_records
WHERE principal_id=? AND operation_id=? AND key=?`, principalID, operationID, key).Scan(&digest); err != nil {
		return fmt.Errorf("read conflicting command completion: %w", err)
	}
	if digest != receipt.RequestDigest {
		return application.ErrKeyReused
	}
	return application.ErrInvalidReceipt
}
