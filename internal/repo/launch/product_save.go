package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/launch"
	"retrom/internal/repo/dbexec"
)

func productCreationSave(
	ctx context.Context,
	executor dbexec.Executor,
	command application.ProductCreateCommand,
) (*application.ProductSave, bool, error) {
	if command.Request.SaveStateID == nil {
		return nil, false, nil
	}
	var save application.ProductSave
	var readable bool
	err := executor.QueryRowContext(ctx, `SELECT save.id,save.profile_id,save.game_id,source.core_id,
 COALESCE(save.payload_blob_id,''),save.checkpoint_format,COALESCE(save.payload_sha256,''),
 COALESCE(save.payload_size_bytes,0),save.dos_entry_path,save.disc_index,
 EXISTS(SELECT 1 FROM game_variants variant
 JOIN runtime_targets target ON target.provider_id=variant.provider_id AND target.target_id=variant.target_id
 WHERE variant.game_id=save.game_id AND variant.status='READY' AND target.checkpoint_json IS NOT NULL
 AND EXISTS(SELECT 1 FROM json_each(target.checkpoint_json,'$.readFormats') readable
 WHERE readable.type='text' AND readable.value=save.checkpoint_format))
FROM save_states save JOIN launch_sessions source ON source.id=save.source_launch_session_id
JOIN games game ON game.id=save.game_id AND game.status='PUBLISHED'
WHERE save.id=? AND save.game_id=? AND save.profile_id=? AND save.deleted_at_ms IS NULL`,
		*command.Request.SaveStateID, command.Request.GameID, command.ProfileID,
	).Scan(
		&save.ID,
		&save.ProfileID,
		&save.GameID,
		&save.SourceCoreID,
		&save.PayloadID,
		&save.Format,
		&save.Digest,
		&save.SizeBytes,
		&save.DOSEntry,
		&save.DiscIndex,
		&readable,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read product save selection: %w", err)
	}
	return &save, readable, nil
}

func productCreationDOS(
	ctx context.Context,
	executor dbexec.Executor,
	gameID string,
	entry *string,
) (application.ProductDOSEntry, error) {
	if entry == nil {
		return application.ProductDOSEntry{}, nil
	}
	var safe int
	err := executor.QueryRowContext(ctx, `SELECT direct_launch_safe FROM dos_entries
WHERE game_id=? AND normalized_path=? AND enabled=1`, gameID, *entry).Scan(&safe)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ProductDOSEntry{}, nil
	}
	if err != nil {
		return application.ProductDOSEntry{}, fmt.Errorf("read product DOS entry: %w", err)
	}
	return application.ProductDOSEntry{Found: true, Safe: safe == 1}, nil
}
