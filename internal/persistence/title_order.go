package persistence

import (
	"context"

	"retrom/internal/format/titleorder"
	"retrom/internal/model"
)

const titleOrderBatch = 500

// RefreshTitleOrder recomputes the letter group and sort key of games written under an older title
// order version. Normal writes keep them current, so this finds rows only after an upgrade.
func (r *Repository) RefreshTitleOrder(ctx context.Context) (int, error) {
	refreshed := 0
	for {
		rows, err := r.db.Query(ctx, `SELECT id,title,title_sort_name FROM game_tab
 WHERE title_order_version<>$1 ORDER BY id LIMIT $2`, titleorder.Version, titleOrderBatch)
		if err != nil {
			return refreshed, failure("read title order", err)
		}
		var ids, titles, names, letters, keys []string
		for rows.Next() {
			var id, title, name string
			if err = rows.Scan(&id, &title, &name); err != nil {
				rows.Close()
				return refreshed, failure("read title order", err)
			}
			order := titleorder.Of(title, name)
			ids, titles, names = append(ids, id), append(titles, title), append(names, name)
			letters, keys = append(letters, order.Letter), append(keys, order.Key)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return refreshed, failure("read title order", err)
		}
		if len(ids) == 0 {
			return refreshed, nil
		}
		// A concurrent edit already stored the current order for its new title; leave it alone.
		if err = r.Execute(ctx, `UPDATE game_tab g SET title_initial=u.letter,title_sort_key=u.sort_key,
 title_order_version=$1 FROM unnest($2::text[],$3::text[],$4::text[],$5::text[],$6::text[])
 AS u(id,title,sort_name,letter,sort_key)
 WHERE g.id=u.id AND g.title=u.title AND g.title_sort_name=u.sort_name AND g.title_order_version<>$1`,
			titleorder.Version, ids, titles, names, letters, keys); err != nil {
			return refreshed, err
		}
		refreshed += len(ids)
	}
}

// GameLetters reports where each letter group starts in the title-ordered list for the same filter.
func (r *Repository) GameLetters(ctx context.Context, userID, status string,
	q model.Query,
) ([]model.TitleLetter, error) {
	if q.TagID != "" {
		if _, err := r.Tag(ctx, q.TagID); err != nil {
			return nil, err
		}
	}
	rows, err := r.db.Query(ctx, `SELECT g.title_initial,count(*),min(g.title_sort_key) AS first`+gameRelations+gameFilter+
		` GROUP BY g.title_initial ORDER BY first`,
		userID, status, q.Search, q.DirectoryID, q.PlatformID, q.TagID, q.Favorite, q.FolderID, q.Unclassified)
	if err != nil {
		return nil, failure("read title letters", err)
	}
	defer rows.Close()
	letters, offset := make([]model.TitleLetter, 0, 27), 0
	for rows.Next() {
		var letter model.TitleLetter
		var first string
		if err = rows.Scan(&letter.Letter, &letter.Count, &first); err != nil {
			return nil, failure("read title letters", err)
		}
		letter.Offset = offset
		offset += letter.Count
		letters = append(letters, letter)
	}
	return letters, failure("read title letters", rows.Err())
}
