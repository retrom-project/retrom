package persistence

import (
	"context"

	"retrom/internal/model"
)

const folderJSON = `jsonb_build_object('id',ff.id,'name',ff.name,'version',ff.version,'gameCount',
 (SELECT count(*) FROM favorite_folder_game_tab fm JOIN game_tab g ON g.id=fm.game_id
 WHERE fm.user_id=ff.user_id AND fm.folder_id=ff.id AND g.status='published'))`

func (r *Repository) Folders(ctx context.Context, userID string) ([]model.Folder, error) {
	return listJSON[model.Folder](ctx, r, "SELECT "+folderJSON+` FROM favorite_folder_tab ff
 WHERE ff.user_id=$1 ORDER BY ff.name_key,ff.id`, userID)
}

func (r *Repository) Folder(ctx context.Context, userID, id string) (model.Folder, error) {
	return readJSON[model.Folder](ctx, r, "SELECT "+folderJSON+
		" FROM favorite_folder_tab ff WHERE ff.user_id=$1 AND ff.id=$2", userID, id)
}

func (r *Repository) WriteFolder(ctx context.Context, userID, id, name string, version, now int64) error {
	if version == 0 {
		return r.Execute(ctx, `INSERT INTO favorite_folder_tab(id,user_id,name,name_key,version,created_at_ms,updated_at_ms)
 VALUES($1,$2,$3,$4,1,$5,$5)`, id, userID, name, model.NameKey(name), now)
	}
	tag, err := r.db.Exec(ctx, `UPDATE favorite_folder_tab SET name=$3,name_key=$4,version=version+1,updated_at_ms=$5
 WHERE id=$1 AND user_id=$2 AND version=$6`, id, userID, name, model.NameKey(name), now, version)
	if err != nil {
		return failure("update favorite folder", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return nil
}

func (r *Repository) DeleteFolder(ctx context.Context, userID, id string) error {
	if _, err := r.Folder(ctx, userID, id); err != nil {
		return err
	}
	if err := r.Execute(ctx,
		"DELETE FROM favorite_folder_game_tab WHERE user_id=$1 AND folder_id=$2",
		userID,
		id); err != nil {
		return err
	}
	return r.Execute(ctx, "DELETE FROM favorite_folder_tab WHERE user_id=$1 AND id=$2", userID, id)
}

func (r *Repository) SetFavorite(ctx context.Context, userID, gameID string, folders []string, now int64) error {
	status, _, err := r.LockGame(ctx, gameID)
	if err != nil {
		return err
	}
	if status != "published" {
		return model.ErrNotFound
	}
	for _, id := range folders {
		if _, err = r.Folder(ctx, userID, id); err != nil {
			return err
		}
	}
	if err = r.Execute(ctx, `INSERT INTO favorite_tab(user_id,game_id,created_at_ms) VALUES($1,$2,$3)
 ON CONFLICT(user_id,game_id) DO NOTHING`, userID, gameID, now); err != nil {
		return err
	}
	if err = r.Execute(ctx,
		"DELETE FROM favorite_folder_game_tab WHERE user_id=$1 AND game_id=$2",
		userID,
		gameID); err != nil {
		return err
	}
	for _, id := range folders {
		if err = r.Execute(ctx, `INSERT INTO favorite_folder_game_tab(user_id,folder_id,game_id,created_at_ms)
 VALUES($1,$2,$3,$4)`, userID, id, gameID, now); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) RemoveFavorite(ctx context.Context, userID, gameID string) error {
	if err := r.Execute(ctx,
		"DELETE FROM favorite_folder_game_tab WHERE user_id=$1 AND game_id=$2",
		userID,
		gameID); err != nil {
		return err
	}
	return r.Execute(ctx, "DELETE FROM favorite_tab WHERE user_id=$1 AND game_id=$2", userID, gameID)
}

func (r *Repository) FavoriteFolders(ctx context.Context, userID, gameID string) ([]string, error) {
	return listJSON[string](ctx, r, `SELECT to_jsonb(folder_id) FROM favorite_folder_game_tab
 WHERE user_id=$1 AND game_id=$2 ORDER BY folder_id`, userID, gameID)
}
