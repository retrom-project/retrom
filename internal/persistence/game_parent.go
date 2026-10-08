package persistence

import (
	"context"
	"encoding/json"

	"retrom/internal/model"
)

// ChangeParent retains unrelated files and the Game's current publication state.
func (r *Repository) ChangeParent(ctx context.Context, id, status string, version int64,
	file model.GameFile, config json.RawMessage, contentHash string, now int64,
) error {
	tag, err := r.db.Exec(ctx, `UPDATE game_tab SET content_hash=$2,runtime_config_json=$3,
 version=version+1,updated_at_ms=$4 WHERE id=$1 AND version=$5 AND status=$6`,
		id, contentHash, config, now, version, status)
	if err != nil {
		return failure("update parent configuration", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	if err = r.Execute(ctx, `UPDATE game_file_tab SET status='deleted',updated_at_ms=$3
 WHERE game_id=$1 AND logical_key=$2 AND status='active'`, id, file.LogicalKey, now); err != nil {
		return err
	}
	return r.InsertFiles(ctx, id, []model.GameFile{file}, now)
}

// ParentCommitted checks the allocated file ID even if the file was subsequently retired.
func (r *Repository) ParentCommitted(ctx context.Context, gameID, fileID string) (bool, error) {
	count, err := r.Count(ctx, "SELECT count(*) FROM game_file_tab WHERE game_id=$1 AND id=$2", gameID, fileID)
	return count == 1, err
}
