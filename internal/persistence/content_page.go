package persistence

import (
	"context"

	"retrom/internal/model"
)

func (r *Repository) GameContentPage(ctx context.Context, id, after, prefix string,
	limit int,
) (model.ContentPage, error) {
	page := model.ContentPage{Items: []model.ContentFile{}}
	rows, err := r.db.Query(ctx, `SELECT logical_key,size_bytes,sha256,media_type FROM (
 SELECT f.logical_key COLLATE "C" AS logical_key,f.size_bytes,f.sha256,'' AS media_type
 FROM game_file_tab f WHERE f.game_id=$1 AND f.status='active' AND f.role<>'project_package'
 UNION ALL SELECT m.logical_path,m.size_bytes,m.sha256,m.media_type FROM game_file_tab f
 JOIN game_file_member_tab m ON m.game_file_id=f.id WHERE f.game_id=$1 AND f.status='active'
 AND f.role='project_package'
) c WHERE logical_key>$2 COLLATE "C" AND starts_with(logical_key,$3) ORDER BY logical_key LIMIT $4`,
		id, after, prefix, limit+1)
	if err != nil {
		return page, failure("page game content", err)
	}
	defer rows.Close()
	for rows.Next() {
		var f model.ContentFile
		if err = rows.Scan(&f.LogicalKey, &f.SizeBytes, &f.SHA256, &f.MediaType); err != nil {
			return page, failure("read content page", err)
		}
		page.Items = append(page.Items, f)
	}
	if err = rows.Err(); err != nil {
		return page, failure("iterate content page", err)
	}
	if len(page.Items) > limit {
		cursor := page.Items[limit-1].LogicalKey
		page.NextCursor = &cursor
		page.Items = page.Items[:limit]
	}
	return page, nil
}
