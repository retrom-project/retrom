package gamecontent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/repo/recordstore"
	"retrom/internal/service/gamecontent"
)

func (writes writes) Enqueue(ctx context.Context, value gamecontent.ScheduleWrite) error {
	_, err := writes.transaction.ExecContext(ctx, `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,
 payload_json,cancellable,state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
 VALUES(?,'GAME',?,'GAME_CONTENT_REPLACE',?,1,'{"schemaVersion":1,"inputExecutionNo":1}',1,'QUEUED',0,2,?,?,?)`,
		value.JobID, value.GameID, value.Dedupe, value.Now, value.Now, value.Now)
	if err != nil {
		return fmt.Errorf("insert replacement job: %w", err)
	}
	_, err = recordstore.CreateUploadConsumptions(ctx, writes.transaction, `INSERT INTO upload_consumptions(
 id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
 VALUES(?,?,NULL,'GAME_CONTENT_REPLACE_JOB',?,?)`, value.ConsumptionID, value.UploadID, value.JobID, value.Now)
	if err != nil {
		return fmt.Errorf("consume replacement upload: %w", err)
	}
	_, err = writes.transaction.ExecContext(
		ctx,
		`INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
 VALUES(?,1,?,?,?)`,
		value.JobID,
		string(value.Input),
		value.InputDigest,
		value.Now,
	)
	if err != nil {
		return fmt.Errorf("insert replacement input: %w", err)
	}
	return writes.event(ctx, value.JobID, "QUEUED", "{}", value.Now)
}

func (writes writes) Load(ctx context.Context, principal, key string, now int64) (gamecontent.Replay, bool, error) {
	_, err := writes.transaction.ExecContext(
		ctx,
		`DELETE FROM idempotency_records
 WHERE operation_id='postAdminGameContentReplacement' AND principal_id=? AND key=? AND expires_at_ms<=?`,
		principal,
		key,
		now,
	)
	if err != nil {
		return gamecontent.Replay{}, false, fmt.Errorf("expire replacement replay: %w", err)
	}
	var result gamecontent.Replay
	err = writes.transaction.QueryRowContext(
		ctx,
		`SELECT request_digest,response_body FROM idempotency_records
 WHERE operation_id='postAdminGameContentReplacement' AND principal_id=? AND key=?`,
		principal,
		key,
	).Scan(
		&result.Digest,
		&result.Body,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return result, false, nil
	}
	if err != nil {
		return result, false, fmt.Errorf("read replacement replay: %w", err)
	}
	return result, true, nil
}

func (writes writes) Remember(ctx context.Context, value gamecontent.ReplayWrite) error {
	_, err := writes.transaction.ExecContext(
		ctx,
		`INSERT INTO idempotency_records(principal_id,operation_id,key,request_digest,
 http_status,response_headers_json,response_body,created_at_ms,expires_at_ms)
 VALUES(?,'postAdminGameContentReplacement',?,?,202,?,?,?,?)`,
		value.PrincipalID,
		value.Key,
		value.Digest,
		string(
			value.Headers,
		),
		value.Body,
		value.Now,
		value.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("remember replacement replay: %w", err)
	}
	return nil
}
