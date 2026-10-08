package persistence

import (
	"context"

	"retrom/internal/model"
)

func (r *Repository) RecordRunning(ctx context.Context, userID, gameID string, now int64) error {
	return r.Execute(ctx, `INSERT INTO recent_game_tab(user_id,game_id,last_played_at_ms)
 SELECT $1,id,$3 FROM game_tab WHERE id=$2 AND status='published'
 ON CONFLICT(user_id,game_id) DO UPDATE SET last_played_at_ms=EXCLUDED.last_played_at_ms`, userID, gameID, now)
}

const recentFilter = ` FROM recent_game_tab rg JOIN game_tab g ON g.id=rg.game_id
 JOIN platform_instance_tab d ON d.id=g.platform_instance_id
 WHERE rg.user_id=$1 AND g.status='published' AND d.enabled AND ($2='' OR d.id=$2)
 AND ($3='' OR g.title ILIKE '%'||$3||'%')
 AND ($4='' OR EXISTS(SELECT 1 FROM game_tag_tab gt JOIN tag_tab t ON t.id=gt.tag_id
 WHERE gt.game_id=g.id AND t.id=$4 AND t.status='active'))
 AND ($5::bigint IS NULL OR rg.last_played_at_ms >= $5)
 AND ($6::bigint IS NULL OR rg.last_played_at_ms <= $6)`

func (r *Repository) RecentGames(ctx context.Context, userID string, q model.Query) (model.Page[model.Recent], error) {
	page := model.Page[model.Recent]{Offset: q.Offset, Limit: q.Limit}
	if q.TagID != "" {
		if _, err := r.Tag(ctx, q.TagID); err != nil {
			return page, err
		}
	}
	order, outerOrder := "rg.last_played_at_ms DESC,g.id", "p.last_played_at_ms DESC,p.game_id"
	if q.Sort == "title" {
		order, outerOrder = "g.title_initial,g.title,g.id", "p.title_initial,p.title,p.game_id"
	}
	args := []any{userID, q.DirectoryID, q.Search, q.TagID, q.AfterMs, q.BeforeMs}
	query := `WITH selected AS MATERIALIZED (SELECT rg.game_id,rg.last_played_at_ms,g.title_initial,g.title` +
		recentFilter + ` ORDER BY ` + order + ` LIMIT $7 OFFSET $8)
 SELECT jsonb_build_object('game',` + gameJSON + `,'lastPlayedAtMs',p.last_played_at_ms)
 FROM selected p JOIN game_tab g ON g.id=p.game_id JOIN platform_instance_tab d ON d.id=g.platform_instance_id
 ORDER BY ` + outerOrder
	items, err := listJSON[model.Recent](ctx, r, query, append(args, q.Limit, q.Offset)...)
	if err != nil {
		return page, err
	}
	count, err := r.Count(ctx, "SELECT count(*)"+recentFilter, args...)
	page.Items, page.Total = items, count
	return page, err
}
