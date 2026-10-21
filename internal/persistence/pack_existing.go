package persistence

import (
	"context"
	"encoding/json"
	"reflect"

	"retrom/internal/model"
)

type ContentSnapshot struct {
	ID          string
	Status      string
	Version     int64
	ContentHash string
	Config      json.RawMessage
	Directory   model.Directory
	Files       []model.GameFile
}

func (r *Repository) PackCandidates(ctx context.Context, after string, limit int) ([]string, error) {
	return listJSON[string](ctx, r, `SELECT to_jsonb(id) FROM game_tab
 WHERE status IN ('pending_review','published')
 AND id>$1 ORDER BY id LIMIT $2`, after, limit)
}

func (r *Repository) ContentSnapshot(ctx context.Context, id string) (ContentSnapshot, error) {
	s := ContentSnapshot{ID: id}
	var directoryID string
	err := r.db.QueryRow(ctx, `SELECT status,version,content_hash,runtime_config_json,platform_instance_id FROM game_tab
 WHERE id=$1 AND status IN ('pending_review','published')`, id).Scan(
		&s.Status, &s.Version, &s.ContentHash, &s.Config, &directoryID)
	if err != nil {
		return s, failure("read storage snapshot", err)
	}
	s.Directory, err = r.Directory(ctx, directoryID)
	if err != nil {
		return s, err
	}
	s.Files, err = r.GameFiles(ctx, id)
	return s, err
}

// SwitchContentStorage changes physical ownership only, preserving the saved configuration and ROM identity.
func (r *Repository) SwitchContentStorage(ctx context.Context, snapshot ContentSnapshot, file model.GameFile,
	now int64,
) error {
	status, version, err := r.LockGame(ctx, snapshot.ID)
	if err != nil {
		return err
	}
	if status != snapshot.Status || version != snapshot.Version {
		return model.ErrConflict
	}
	files, err := r.GameFiles(ctx, snapshot.ID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(files, snapshot.Files) {
		return model.ErrConflict
	}
	if err = r.Execute(ctx, `UPDATE game_file_tab SET status='deleted',updated_at_ms=$2
 WHERE game_id=$1 AND status='active'`, snapshot.ID, now); err != nil {
		return err
	}
	if err = r.InsertFiles(ctx, snapshot.ID, []model.GameFile{file}, now); err != nil {
		return err
	}
	return r.BumpGame(ctx, snapshot.ID, snapshot.Version, now)
}
