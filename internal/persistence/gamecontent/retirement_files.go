package gamecontent

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	"retrom/internal/persistence/filedeletion"
)

func (records retirementRecords) RetireContent(ctx context.Context, gameID string, now int64) error {
	values, err := dbapi.QueryStrings(ctx, records.executor, `SELECT file_record FROM game_files WHERE game_id=$1
 UNION SELECT f.file_record FROM variant_files f JOIN game_variants v ON v.id=f.game_variant_id
 WHERE v.game_id=$1 AND f.role<>'BIOS_BUNDLE'`, gameID)
	if err != nil {
		return fmt.Errorf("retire content: %w", err)
	}
	seen := map[string]bool{}
	for _, value := range values {
		directory, err := filestore.CleanupDirectory(value)
		if err != nil {
			return fmt.Errorf("read replaced content directory: %w", err)
		}
		if directory == "" || seen[directory] {
			continue
		}
		seen[directory] = true
		if err := filedeletion.QueuePath(ctx, records.executor, directory, now); err != nil {
			return fmt.Errorf("retire content: %w", err)
		}
	}
	ids, err := dbapi.QueryStrings(ctx, records.executor, `SELECT id FROM save_states WHERE game_id=?`, gameID)
	if err != nil {
		return fmt.Errorf("retire content: %w", err)
	}
	for _, id := range ids {
		if err := filedeletion.QueuePath(ctx, records.executor, "saves/"+id, now); err != nil {
			return fmt.Errorf("retire content: %w", err)
		}
	}
	return nil
}
