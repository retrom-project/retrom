package saves

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/service/saves"
)

func (store records) Replay(ctx context.Context, key saves.ReplayKey) (saves.Replay, bool, error) {
	var replay saves.Replay
	err := store.executor.QueryRowContext(ctx, `
SELECT request_digest,response_body FROM idempotency_records
WHERE operation_id='postRuntimeSaveState' AND key=? AND principal_id=? AND expires_at_ms>?`,
		key.Key, key.PrincipalID, key.AtMS).Scan(&replay.Digest, &replay.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return saves.Replay{}, false, nil
	}
	if err != nil {
		return saves.Replay{}, false, fmt.Errorf("read checkpoint idempotency: %w", err)
	}
	return replay, true, nil
}

func (store records) Remember(ctx context.Context, replay saves.ReplayWrite) error {
	if _, err := store.executor.ExecContext(ctx, `
INSERT INTO idempotency_records(principal_id,operation_id,key,request_digest,http_status,
 response_headers_json,response_body,created_at_ms,expires_at_ms)
VALUES(?,'postRuntimeSaveState',?,?,201,'{}',?,?,?)`,
		replay.PrincipalID, replay.Key, replay.Digest, replay.Body, replay.AtMS, replay.ExpiresAtMS); err != nil {
		return fmt.Errorf("insert checkpoint idempotency: %w", err)
	}
	return nil
}
