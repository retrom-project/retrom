package gamecontent

import (
	"context"
	"fmt"

	"retrom/internal/persistence/uploads/receivedfiles"
)

func (records retirementRecords) RetireContent(ctx context.Context, gameID string, now int64) error {
	_, err := records.executor.ExecContext(ctx, `UPDATE stored_files SET retired_at_ms=COALESCE(retired_at_ms,?2)
 WHERE (owner_kind='GAME' AND owner_id=?1 AND id IN(
 SELECT blob_id FROM game_files WHERE game_id=?1 UNION SELECT file.blob_id FROM variant_files file
 JOIN game_variants variant ON variant.id=file.game_variant_id WHERE variant.game_id=?1 AND file.role<>'BIOS_BUNDLE'))
 OR (owner_kind='SAVE_STATE' AND owner_id IN(SELECT id FROM save_states WHERE game_id=?1))`, gameID, now)
	if err != nil {
		return fmt.Errorf("retire replaced content files: %w", err)
	}
	if err := receivedfiles.ReleaseRetired(ctx, records.executor, "GAME", gameID, now); err != nil {
		return fmt.Errorf("release retired content inputs: %w", err)
	}
	return nil
}
