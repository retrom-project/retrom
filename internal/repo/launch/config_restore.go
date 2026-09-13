package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/launch"
)

func configRestore(
	ctx context.Context,
	executor dbexec.Executor,
	ref application.SessionRef,
	source application.ConfigSource,
) (application.ConfigRestore, error) {
	if ref.Preview {
		return configPreviewRestore(ctx, executor, ref.ID)
	}
	if source.SaveID == nil {
		return application.ConfigRestore{}, nil
	}
	result := application.ConfigRestore{Required: true}
	err := executor.QueryRowContext(ctx, `
SELECT save.checkpoint_format,blob.sha256,blob.size_bytes
FROM save_states save
LEFT JOIN launch_game_save_bindings binding ON binding.launch_session_id=?
JOIN blobs blob ON blob.id=COALESCE(binding.restore_payload_blob_id,save.payload_blob_id)
WHERE save.id=? AND save.deleted_at_ms IS NULL`, ref.ID, *source.SaveID).
		Scan(&result.Format, &result.Digest, &result.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return application.ConfigRestore{}, fmt.Errorf("read config restore: %w", err)
	}
	result.Found = true
	return result, nil
}

func configPreviewRestore(ctx context.Context, executor dbexec.Executor, id string) (application.ConfigRestore, error) {
	var payload, format, digest sql.NullString
	var size sql.NullInt64
	err := executor.QueryRowContext(ctx, `
SELECT preview.restore_payload_blob_id,preview.restore_checkpoint_format,blob.sha256,blob.size_bytes
FROM review_preview_sessions preview LEFT JOIN blobs blob ON blob.id=preview.restore_payload_blob_id
WHERE preview.id=?`, id).Scan(&payload, &format, &digest, &size)
	if err != nil {
		return application.ConfigRestore{}, fmt.Errorf("read preview config restore: %w", err)
	}
	return application.ConfigRestore{
		Required: payload.Valid, Found: payload.Valid && format.Valid && digest.Valid && size.Valid,
		Format: format.String, Digest: digest.String, Size: size.Int64,
	}, nil
}

func configIsolation(
	ctx context.Context,
	executor dbexec.Executor,
	ref application.SessionRef,
) ([]application.IsolationGrant, error) {
	column := "launch_id"
	if ref.Preview {
		column = "preview_id"
	}
	rows, err := executor.QueryContext(ctx, `
SELECT expected_origin,ticket_sha256,expires_at_ms FROM isolated_runtime_bootstrap_tickets
WHERE `+column+`=? AND consumed_at_ms IS NULL
UNION ALL
SELECT expected_origin,NULL,expires_at_ms FROM isolated_runtime_capabilities
WHERE `+column+`=? AND revoked_at_ms IS NULL`, ref.ID, ref.ID)
	if err != nil {
		return nil, fmt.Errorf("read config isolation grants: %w", err)
	}
	defer func() { cleanup.Error("close isolation grants", rows.Close()) }()
	grants := make([]application.IsolationGrant, 0)
	for rows.Next() {
		var grant application.IsolationGrant
		if err := rows.Scan(&grant.Origin, &grant.TicketHash, &grant.ExpiresAtMS); err != nil {
			return nil, fmt.Errorf("scan config isolation grant: %w", err)
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate config isolation grants: %w", err)
	}
	return grants, nil
}
