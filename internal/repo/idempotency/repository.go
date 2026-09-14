package idempotency

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/idempotency"
)

type Repository struct {
	database *sql.DB
}

func New(database *sql.DB) *Repository {
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
	err := repository.database.QueryRowContext(ctx, `
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

func (repository *Repository) Save(
	ctx context.Context,
	operationID, key, principalID string,
	receipt application.Receipt,
	createdAtMS, expiresAtMS int64,
) error {
	if _, err := repository.database.ExecContext(ctx, `
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
`, principalID, operationID, key, receipt.RequestDigest, receipt.HTTPStatus,
		receipt.HeadersJSON, receipt.Body, createdAtMS, expiresAtMS); err != nil {
		return fmt.Errorf("write idempotency receipt: %w", err)
	}
	return nil
}
