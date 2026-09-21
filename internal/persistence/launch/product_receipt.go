package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/service/launch"
)

func (records productCreationRecords) Replay(
	ctx context.Context,
	command application.ProductCreateCommand,
) (application.ProductReceipt, bool, error) {
	if command.Key == "" {
		return application.ProductReceipt{}, false, nil
	}
	var receipt application.ProductReceipt
	err := records.executor.QueryRowContext(ctx, `
SELECT request_digest,http_status,response_body,created_at_ms,expires_at_ms
FROM idempotency_records WHERE operation_id='postLaunch' AND principal_id=? AND key=?`,
		command.ActorID, command.Key,
	).Scan(
		&receipt.Digest,
		&receipt.Status,
		&receipt.Body,
		&receipt.CreatedAtMS,
		&receipt.ExpiresAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ProductReceipt{}, false, nil
	}
	if err != nil {
		return application.ProductReceipt{}, false, fmt.Errorf("read product receipt: %w", err)
	}
	return receipt, true, nil
}

func (records productCreationRecords) StoreReceipt(
	ctx context.Context,
	command application.ProductCreateCommand,
	receipt application.ProductReceipt,
) error {
	if command.Key == "" {
		return nil
	}
	if _, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO idempotency_records(
principal_id,operation_id,key,request_digest,http_status,response_headers_json,
response_body,created_at_ms,expires_at_ms)
VALUES(?,'postLaunch',?,?,?,'{}',?,?,?)`,
		command.ActorID,
		command.Key,
		command.Digest,
		receipt.Status,
		[]byte(receipt.Body),
		receipt.CreatedAtMS,
		receipt.ExpiresAtMS,
	); err != nil {
		return fmt.Errorf("store product receipt: %w", err)
	}
	return nil
}
