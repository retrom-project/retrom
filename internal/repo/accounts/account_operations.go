package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/repo/dbexec"
	"retrom/internal/service/accounts"
)

type accountOperations struct{ executor dbexec.Executor }

func (records accountOperations) Replay(
	ctx context.Context,
	operation accounts.AccountOperation,
) (accounts.AccountReplay, error) {
	var replay accounts.AccountReplay
	err := records.executor.QueryRowContext(
		ctx,
		`SELECT request_digest,response_body FROM idempotency_records
 WHERE principal_id=? AND operation_id=? AND key=? AND expires_at_ms>?`,
		operation.PrincipalID,
		operation.Operation,
		operation.Key,
		operation.Now,
	).Scan(
		&replay.Digest,
		&replay.Body,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return replay, nil
	}
	if err != nil {
		return replay, fmt.Errorf("read account replay: %w", err)
	}
	replay.Found = true
	return replay, nil
}

func (records accountOperations) Remember(ctx context.Context, receipt accounts.AccountReceipt) error {
	operation := receipt.Operation
	_, err := records.executor.ExecContext(
		ctx,
		`DELETE FROM idempotency_records
 WHERE principal_id=? AND operation_id=? AND key=? AND expires_at_ms<=?`,
		operation.PrincipalID,
		operation.Operation,
		operation.Key,
		operation.Now,
	)
	if err != nil {
		return fmt.Errorf("expire account replay: %w", err)
	}
	body := receipt.Body
	if body == nil {
		body = []byte{}
	}
	_, err = records.executor.ExecContext(
		ctx,
		`INSERT INTO idempotency_records(principal_id,operation_id,key,request_digest,http_status,
 response_headers_json,response_body,created_at_ms,expires_at_ms) VALUES(?,?,?,?,?,'{}',?,?,?)`,
		operation.PrincipalID,
		operation.Operation,
		operation.Key,
		operation.Digest,
		receipt.Status,
		body,
		operation.Now,
		receipt.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("write account replay: %w", err)
	}
	return nil
}

func (records accountOperations) Audit(ctx context.Context, audit accounts.AccountAudit) error {
	_, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
 before_json,after_json,diff_json,request_id,created_at_ms) VALUES(?,'USER',?,NULL,?,?,?,?,?,'{}',NULL,?)`,
		audit.ID,
		audit.ActorID,
		audit.Action,
		audit.ResourceType,
		audit.ResourceID,
		audit.BeforeJSON,
		audit.AfterJSON,
		audit.Now,
	)
	if err != nil {
		return fmt.Errorf("insert account audit: %w", err)
	}
	return nil
}
