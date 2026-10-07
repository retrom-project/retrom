package persistence

import (
	"context"

	"retrom/internal/model"
)

const tagJSON = `jsonb_build_object('id',t.id,'name',t.name,'version',t.version,'gameCount',
 (SELECT count(*) FROM game_tag_tab gt JOIN game_tab g ON g.id=gt.game_id
 WHERE gt.tag_id=t.id AND g.status='published'))`

func (r *Repository) Tags(ctx context.Context, q model.Query) (model.Page[model.Tag], error) {
	page := model.Page[model.Tag]{Offset: q.Offset, Limit: q.Limit}
	items, err := listJSON[model.Tag](ctx, r, "SELECT "+tagJSON+` FROM tag_tab t
 WHERE status='active' AND name ILIKE '%'||$1||'%' ORDER BY name_key,
id LIMIT $2 OFFSET $3`,
		q.Search,
		q.Limit,
		q.Offset)
	if err != nil {
		return page, err
	}
	count, err := r.Count(ctx, "SELECT count(*) FROM tag_tab WHERE status='active' AND name ILIKE '%'||$1||'%'", q.Search)
	page.Items = items
	page.Total = count
	return page, err
}

func (r *Repository) Tag(ctx context.Context, id string) (model.Tag, error) {
	return readJSON[model.Tag](ctx, r, "SELECT "+tagJSON+" FROM tag_tab t WHERE id=$1 AND status='active'", id)
}

func (r *Repository) WriteTag(ctx context.Context, id, userID, name string, version, now int64) error {
	if version == 0 {
		return r.Execute(ctx, `INSERT INTO tag_tab
 (id,name,name_key,status,version,created_by_user_id,updated_by_user_id,created_at_ms,updated_at_ms)
 VALUES($1,$2,$3,'active',1,$4,$4,$5,$5)`, id, name, model.NameKey(name), userID, now)
	}
	tag, err := r.db.Exec(ctx, `UPDATE tag_tab SET name=$2,name_key=$3,version=version+1,updated_by_user_id=$4,
 updated_at_ms=$5 WHERE id=$1 AND version=$6 AND status='active'`, id, name, model.NameKey(name), userID, now, version)
	if err != nil {
		return failure("update tag", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return nil
}

func (r *Repository) DeleteTag(ctx context.Context, id, userID string, version, now int64) error {
	tag, err := r.db.Exec(ctx, `UPDATE tag_tab SET status='deleted',version=version+1,updated_by_user_id=$2,
 updated_at_ms=$3 WHERE id=$1 AND version=$4 AND status='active'`, id, userID, now, version)
	if err != nil {
		return failure("delete tag", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return nil
}

func (r *Repository) AssignTags(ctx context.Context, gameID, userID string, ids []string, now int64) error {
	if err := model.ValidateTagIDs(ids); err != nil {
		return failure("validate assigned tags", err)
	}
	for _, id := range ids {
		if _, err := r.Tag(ctx, id); err != nil {
			return err
		}
	}
	if err := r.Execute(ctx, "DELETE FROM game_tag_tab WHERE game_id=$1", gameID); err != nil {
		return err
	}
	for _, id := range ids {
		if err := r.Execute(ctx, `INSERT INTO game_tag_tab(game_id,tag_id,assigned_by_user_id,created_at_ms)
 VALUES($1,$2,$3,$4)`, gameID, id, userID, now); err != nil {
			return err
		}
	}
	return nil
}
