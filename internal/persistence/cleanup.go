package persistence

import (
	"context"

	"retrom/internal/model"
)

func (r *Repository) DeletedGames(ctx context.Context, before int64) ([]string, error) {
	return listJSON[string](ctx, r,
		`SELECT to_jsonb(id) FROM game_tab WHERE status='deleted' AND updated_at_ms<$1 ORDER BY updated_at_ms,id LIMIT
50`, before)
}

func (r *Repository) GameSaveIDs(ctx context.Context, id string) ([]string, error) {
	return listJSON[string](ctx, r, "SELECT to_jsonb(id) FROM save_tab WHERE game_id=$1", id)
}

func (r *Repository) PurgeGame(ctx context.Context, id string, now int64) error {
	status, _, err := r.LockGame(ctx, id)
	if err != nil {
		return err
	}
	if status != "deleted" {
		return model.ErrConflict
	}
	for _, table := range []string{
		"save_tab", "recent_game_tab", "favorite_folder_game_tab", "favorite_tab",
		"game_tag_tab", "game_file_tab", "game_media_tab",
	} {
		if err = r.Execute(ctx, "DELETE FROM "+table+" WHERE game_id=$1", id); err != nil {
			return err
		}
	}
	return r.Execute(ctx, "UPDATE game_tab SET status='purged',updated_at_ms=$2 WHERE id=$1 AND status='deleted'", id, now)
}

func (r *Repository) RetiredFiles(ctx context.Context, before int64) ([]model.RetiredFile, error) {
	result := make([]model.RetiredFile, 0)
	for _, table := range []string{"game_file_tab", "game_media_tab", "bios_file_tab", "save_tab"} {
		screenshot := "''"
		key := "storage_key"
		if table == "save_tab" {
			screenshot = "COALESCE(screenshot_key,'')"
			key = "''"
		}
		query := `SELECT jsonb_build_object('Table',$2::text,'ID',id,'Key',` + key +
			`,'ScreenshotKey',` + screenshot + `) FROM ` + table +
			` WHERE status='deleted' AND updated_at_ms<$1 ORDER BY updated_at_ms,id LIMIT 100`
		items, err := listJSON[model.RetiredFile](ctx, r, query, before, table)
		if err != nil {
			return nil, err
		}
		result = append(result, items...)
	}
	return result, nil
}

func (r *Repository) PurgeFile(ctx context.Context, file model.RetiredFile, now int64) error {
	if !model.OneOf(file.Table, "game_file_tab", "game_media_tab", "bios_file_tab", "save_tab") {
		return model.ErrInvalid
	}
	query := `UPDATE ` + file.Table + ` SET status='purged',updated_at_ms=$2 WHERE id=$1 AND status='deleted'`
	return r.Execute(ctx, query, file.ID, now)
}

func (r *Repository) Referenced(ctx context.Context, key string) (bool, error) {
	count, err := r.Count(ctx, `SELECT
 (SELECT count(*) FROM game_file_tab WHERE storage_key=$1 AND status='active')+
 (SELECT count(*) FROM game_media_tab WHERE storage_key=$1 AND status='active')+
 (SELECT count(*) FROM bios_file_tab WHERE storage_key=$1 AND status='active')+
 (SELECT count(*) FROM save_tab WHERE (storage_key=$1 OR screenshot_key=$1) AND status='active')`, key)
	return count > 0, err
}

func (r *Repository) DeletedUsers(ctx context.Context) ([]string, error) {
	return listJSON[string](ctx, r, `SELECT to_jsonb(u.id) FROM user_tab u WHERE u.status='deleted' AND (
 EXISTS(SELECT 1 FROM save_tab WHERE user_id=u.id AND status='active') OR
 EXISTS(SELECT 1 FROM favorite_tab WHERE user_id=u.id) OR
 EXISTS(SELECT 1 FROM favorite_folder_tab WHERE user_id=u.id) OR
 EXISTS(SELECT 1 FROM favorite_folder_game_tab WHERE user_id=u.id) OR
 EXISTS(SELECT 1 FROM recent_game_tab WHERE user_id=u.id)) ORDER BY u.id LIMIT 50`)
}

func (r *Repository) RetireUserResources(ctx context.Context, id string, now int64) error {
	if err := r.Execute(ctx, `UPDATE save_tab SET status='deleted',updated_at_ms=$2
 WHERE user_id=$1 AND status='active'`, id, now); err != nil {
		return err
	}
	for _, table := range []string{"favorite_folder_game_tab", "favorite_tab", "favorite_folder_tab", "recent_game_tab"} {
		if err := r.Execute(ctx, "DELETE FROM "+table+" WHERE user_id=$1", id); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ExpireScanProgress(ctx context.Context, before int64) error {
	return r.Execute(ctx, `DELETE FROM scan_progress_tab WHERE id IN (
 SELECT id FROM scan_progress_tab WHERE status<>'running' AND updated_at_ms<$1
 ORDER BY updated_at_ms,id LIMIT 200)`, before)
}
