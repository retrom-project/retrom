package runtimeprovider

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
)

func validateCheckpointFormats(
	ctx context.Context, transaction *sql.Tx, providerID string, target targetProjection, now int64,
) error {
	readable := make(map[string]bool)
	if target.target.Checkpoint != nil {
		for _, format := range target.target.Checkpoint.ReadFormats {
			readable[format] = true
		}
	}
	rows, err := transaction.QueryContext(ctx, `
WITH selected(provider_id,target_id,now_ms) AS (VALUES(?,?,?))
SELECT DISTINCT save.checkpoint_format FROM save_states save
JOIN game_variants variant ON variant.game_id=save.game_id
JOIN selected ON selected.provider_id=variant.provider_id AND selected.target_id=variant.target_id
WHERE save.deleted_at_ms IS NULL
UNION
SELECT checkpoint.checkpoint_format FROM rpgmaker_runtime_validation_checkpoints checkpoint
JOIN rpgmaker_runtime_validations validation ON validation.id=checkpoint.validation_id
JOIN selected ON selected.provider_id=validation.provider_id AND selected.target_id=validation.target_id
WHERE validation.state NOT IN ('PASSED','FAILED','EXPIRED') AND validation.expires_at_ms>selected.now_ms
`, providerID, target.target.ID, now)
	if err != nil {
		return fmt.Errorf("read active checkpoint formats: %w", err)
	}
	defer func() { cleanup.Error("close active checkpoint formats", rows.Close()) }()
	for rows.Next() {
		var format string
		if err := rows.Scan(&format); err != nil {
			return fmt.Errorf("scan active checkpoint format: %w", err)
		}
		if !readable[format] {
			return fmt.Errorf("%w: %s/%s %s", ErrProviderCheckpointUnreadable, providerID, target.target.ID, format)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("active checkpoint formats: %w", err)
	}
	return nil
}

// Finalizing expired reviews releases their temporary checkpoint references via
// the lifecycle trigger. Blob GC, not catalog synchronization, owns CAS removal.
func expireReviewCheckpointPayloads(ctx context.Context, transaction *sql.Tx, now int64) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions SET state='EXPIRED',finished_at_ms=?,updated_at_ms=?,version=version+1
WHERE purpose='RPG_RUNTIME_VALIDATION' AND state IN ('CREATED','ACTIVE')
AND rpgmaker_runtime_validation_id IN (
 SELECT id FROM rpgmaker_runtime_validations WHERE expires_at_ms<=? AND state NOT IN ('PASSED','FAILED','EXPIRED')
)
`, now, now, now); err != nil {
		return fmt.Errorf("expire review launches before provider sync: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE rpgmaker_runtime_validations SET state='EXPIRED',failure_code='RPG_RUNTIME_TIMEOUT',updated_at_ms=?
WHERE expires_at_ms<=? AND state NOT IN ('PASSED','FAILED','EXPIRED')
`, now, now); err != nil {
		return fmt.Errorf("expire review payloads before provider sync: %w", err)
	}
	return nil
}
