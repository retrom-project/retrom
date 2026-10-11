package persistence

import (
	"context"

	"retrom/internal/model"
)

// Called after the game's version CAS, inside the same short transaction.
func (r *Repository) RetireParentFiles(ctx context.Context, id, hash string, retired []string, now int64) error {
	if err := r.Execute(ctx, `UPDATE game_tab SET content_hash=$2 WHERE id=$1`, id, hash); err != nil {
		return err
	}
	for _, fileID := range retired {
		tag, err := r.db.Exec(ctx, `UPDATE game_file_tab SET status='deleted',updated_at_ms=$3
 WHERE game_id=$1 AND id=$2 AND status='active'`, id, fileID, now)
		if err != nil {
			return failure("retire parent file", err)
		}
		if tag.RowsAffected() != 1 {
			return model.ErrConflict
		}
	}
	return nil
}
