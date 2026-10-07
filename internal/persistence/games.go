package persistence

import (
	"context"
	"fmt"

	"retrom/internal/model"
)

const gameJSON = `jsonb_build_object('id',g.id,'platformInstanceId',g.platform_instance_id,'platformId',d.platform_id,
 'directoryName',d.name,'title',g.title,'description',g.description,'developer',g.developer,'publisher',g.publisher,
 'genre',
g.genre,
'players',
nullif(g.players,
''),
'releaseYear',
g.release_year,
'status',
g.status,
'source',
g.source,
'version',
g.version,

 'contentHash',g.content_hash,'createdAtMs',g.created_at_ms,'updatedAtMs',g.updated_at_ms,
 'favorite',EXISTS(SELECT 1 FROM favorite_tab f WHERE f.user_id=$1 AND f.game_id=g.id),
 'tags',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',t.id,'name',t.name,'version',t.version,'gameCount',0)
 ORDER BY t.name_key,t.id) FROM game_tag_tab gt JOIN tag_tab t ON t.id=gt.tag_id
 WHERE gt.game_id=g.id AND t.status='active'),'[]'::jsonb),
 'media',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',m.id,'kind',m.kind,'ordinal',m.ordinal,
 'mediaType',m.media_type,'url','/api/v1/games/'||g.id||'/media/'||m.id) ORDER BY m.kind,m.ordinal)
 FROM game_media_tab m WHERE m.game_id=g.id AND m.status='active'),'[]'::jsonb))`

const gameRelations = ` FROM game_tab g JOIN platform_instance_tab d ON d.id=g.platform_instance_id`

const gameFilter = ` WHERE g.status=$2 AND ($2<>'published' OR d.enabled)
 AND ($3='' OR g.title ILIKE '%'||$3||'%') AND ($4='' OR d.id=$4) AND ($5='' OR d.platform_id=$5)
 AND ($6='' OR EXISTS(SELECT 1 FROM game_tag_tab gt JOIN tag_tab t ON t.id=gt.tag_id
 WHERE gt.game_id=g.id AND t.id=$6 AND t.status='active'))
 AND (NOT $7 OR EXISTS(SELECT 1 FROM favorite_tab f WHERE f.game_id=g.id AND f.user_id=$1))
 AND ($8='' OR EXISTS(SELECT 1 FROM favorite_folder_game_tab ff WHERE ff.user_id=$1 AND ff.folder_id=$8
 AND ff.game_id=g.id))
 AND (NOT $9 OR NOT EXISTS(SELECT 1 FROM favorite_folder_game_tab ff WHERE ff.user_id=$1 AND ff.game_id=g.id))`

func (r *Repository) Games(ctx context.Context, userID, status string, q model.Query) (model.Page[model.Game], error) {
	page := model.Page[model.Game]{Offset: q.Offset, Limit: q.Limit}
	if q.TagID != "" {
		if _, err := r.Tag(ctx, q.TagID); err != nil {
			return page, err
		}
	}
	args := []any{userID, status, q.Search, q.DirectoryID, q.PlatformID, q.TagID, q.Favorite, q.FolderID, q.Unclassified}
	order, projection, relations := "g.title_initial,g.title,g.id", "g.id,g.title_initial,g.title", gameRelations
	outerOrder := "p.title_initial,p.title,p.id"
	if q.Sort == "recent" {
		relations += " LEFT JOIN recent_game_tab rg ON rg.game_id=g.id AND rg.user_id=$1"
		projection += ",rg.last_played_at_ms"
		order = "rg.last_played_at_ms DESC NULLS LAST,g.title_initial,g.title,g.id"
		outerOrder = "p.last_played_at_ms DESC NULLS LAST,p.title_initial,p.title,p.id"
	}
	query := `WITH selected AS MATERIALIZED (SELECT ` + projection + relations + gameFilter +
		` ORDER BY ` + order + ` LIMIT $10 OFFSET $11)
 SELECT ` + gameJSON + ` FROM selected p JOIN game_tab g ON g.id=p.id
 JOIN platform_instance_tab d ON d.id=g.platform_instance_id ORDER BY ` + outerOrder
	items, err := listJSON[model.Game](ctx, r, query, append(args, q.Limit, q.Offset)...)
	if err != nil {
		return page, err
	}
	count, err := r.Count(ctx, "SELECT count(*)"+gameRelations+gameFilter, args...)
	page.Items = items
	page.Total = count
	return page, err
}

func (r *Repository) Game(ctx context.Context, userID, id, status string) (model.Game, error) {
	return readJSON[model.Game](ctx, r, "SELECT "+gameJSON+` FROM game_tab g
 JOIN platform_instance_tab d ON d.id=g.platform_instance_id WHERE g.id=$2 AND g.status=$3
 AND ($3<>'published' OR d.enabled)`,
		userID,
		id,
		status)
}

func (r *Repository) GameAccessible(ctx context.Context, id, status string) error {
	count, err := r.Count(ctx, `SELECT count(*) FROM game_tab g JOIN platform_instance_tab d ON d.id=g.platform_instance_id
 WHERE g.id=$1 AND g.status=$2 AND ($2<>'published' OR d.enabled)`, id, status)
	if err != nil {
		return err
	}
	if count != 1 {
		return model.ErrNotFound
	}
	return nil
}

func (r *Repository) GameDetail(ctx context.Context, userID, id, status string) (model.GameDetail, error) {
	result := model.GameDetail{Saves: []model.Save{}, Files: []model.GameFile{}, FavoriteFolderIDs: []string{}}
	game, err := r.Game(ctx, userID, id, status)
	if err != nil {
		return result, err
	}
	result.Game = game
	directory, err := r.Directory(ctx, game.PlatformInstanceID)
	if err != nil {
		return result, err
	}
	result.CoreIDs = directory.CoreIDs
	result.DefaultCoreID = directory.DefaultCoreID
	if err = r.db.QueryRow(ctx,
		"SELECT runtime_config_json FROM game_tab WHERE id=$1",
		id).Scan(&result.RuntimeConfig); err != nil {
		return result, failure("read runtime configuration", err)
	}
	files, err := r.GameFiles(ctx, id)
	result.Files = files
	return result, err
}

func (r *Repository) GameFiles(ctx context.Context, id string) ([]model.GameFile, error) {
	rows, err := r.db.Query(ctx, `SELECT id,logical_key,role,size_bytes,sha256,storage_key
 FROM game_file_tab WHERE game_id=$1 AND status='active' ORDER BY logical_key`, id)
	if err != nil {
		return nil, failure("list game files", err)
	}
	defer rows.Close()
	files := make([]model.GameFile, 0)
	for rows.Next() {
		var file model.GameFile
		if err = rows.Scan(&file.ID,
			&file.LogicalKey,
			&file.Role,
			&file.SizeBytes,
			&file.SHA256,
			&file.StorageKey); err != nil {
			return nil, fmt.Errorf("read game file: %w", err)
		}
		files = append(files, file)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate game files: %w", err)
	}
	return files, nil
}

func (r *Repository) MediaKey(ctx context.Context, gameID, mediaID string) (string, error) {
	var key string
	err := r.db.QueryRow(ctx, "SELECT storage_key FROM game_media_tab WHERE id=$1 AND game_id=$2 AND status='active'",
		mediaID, gameID).Scan(&key)
	if err != nil {
		return "", failure("read media", err)
	}
	return key, nil
}

func (r *Repository) LockGame(ctx context.Context, id string) (string, int64, error) {
	var status string
	var version int64
	err := r.db.QueryRow(ctx, "SELECT status,version FROM game_tab WHERE id=$1 FOR UPDATE", id).Scan(&status, &version)
	if err != nil {
		return "", 0, failure("lock game", err)
	}
	return status, version, nil
}

func (r *Repository) PublishedGameCount(ctx context.Context) (int64, error) {
	return r.Count(ctx, `SELECT count(*) FROM game_tab g JOIN platform_instance_tab d ON d.id=g.platform_instance_id
 WHERE g.status='published' AND d.enabled`)
}
