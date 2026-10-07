package persistence

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/model"
)

const saveJSON = `jsonb_build_object('id',
s.id,
'game',
` + gameJSON + `,
'kind',
s.save_type,
'name',
s.name,
'slot',
s.slot_key,

 'version',s.version,'sizeBytes',s.payload_size_bytes,'createdAtMs',s.created_at_ms,'updatedAtMs',s.updated_at_ms,
 'screenshotUrl',CASE WHEN s.screenshot_key IS NULL THEN NULL ELSE '/api/v1/saves/'||s.id||'/screenshot' END,
 'restorable',false,'restoreReason','CORE_UNAVAILABLE','extinfo',s.extinfo)`

func (r *Repository) Saves(ctx context.Context, userID, gameID string, q model.Query) (model.SavePage, error) {
	page := model.SavePage{Page: model.Page[model.Save]{Offset: q.Offset, Limit: q.Limit}}
	filter := ` FROM save_tab s JOIN game_tab g ON g.id=s.game_id
 JOIN platform_instance_tab d ON d.id=g.platform_instance_id
 WHERE s.user_id=$1 AND s.status='active' AND ($2='' OR s.game_id=$2)
 AND ($3='' OR g.title ILIKE '%'||$3||'%' OR s.name ILIKE '%'||$3||'%') AND ($4='' OR s.save_type=$4)`
	order := "s.updated_at_ms DESC,s.id"
	outerOrder := "p.updated_at_ms DESC,p.id"
	if q.Sort == "title" {
		order = "g.title_initial,g.title,s.name,s.id"
		outerOrder = "p.title_initial,p.title,p.name,p.id"
	}
	query := `WITH selected AS MATERIALIZED (SELECT s.id,s.updated_at_ms,g.title_initial,g.title,s.name` + filter +
		` ORDER BY ` + order + ` LIMIT $5 OFFSET $6)
 SELECT ` + saveJSON + ` FROM selected p JOIN save_tab s ON s.id=p.id
 JOIN game_tab g ON g.id=s.game_id JOIN platform_instance_tab d ON d.id=g.platform_instance_id
 ORDER BY ` + outerOrder
	args := []any{userID, gameID, q.Search, q.Kind}
	items, err := listJSON[model.Save](ctx, r, query, append(args, q.Limit, q.Offset)...)
	if err != nil {
		return page, err
	}
	page.Items = items
	page.Total, err = r.Count(ctx, "SELECT count(*)"+filter, args...)
	if err != nil {
		return page, err
	}
	page.GameCount, err = r.Count(ctx, "SELECT count(DISTINCT s.game_id)"+filter, args...)
	return page, err
}

func (r *Repository) Save(ctx context.Context, userID, id string) (model.Save, error) {
	var item model.Save
	var data []byte
	err := r.db.QueryRow(ctx, "SELECT "+saveJSON+`,s.storage_key,COALESCE(s.screenshot_key,''),
 s.payload_hash,s.last_commit_id FROM save_tab s JOIN game_tab g ON g.id=s.game_id
 JOIN platform_instance_tab d ON d.id=g.platform_instance_id
 WHERE s.user_id=$1 AND s.id=$2 AND s.status='active'`, userID, id).
		Scan(&data, &item.StorageKey, &item.ScreenshotKey, &item.PayloadHash, &item.LastCommitID)
	if err != nil {
		return item, failure("read save files", err)
	}
	if err = json.Unmarshal(data, &item); err != nil {
		return item, fmt.Errorf("decode save: %w", err)
	}
	item.UserID = userID
	return item, nil
}

func (r *Repository) WriteSave(ctx context.Context, save model.Save, now int64, create bool) error {
	info, err := json.Marshal(save.Extinfo)
	if err != nil {
		return failure("encode save context", err)
	}
	var screenshot *string
	if save.ScreenshotKey != "" {
		screenshot = &save.ScreenshotKey
	}
	if create {
		return r.Execute(ctx,
			`INSERT INTO save_tab(id,
user_id,
game_id,
save_type,
name,
slot_key,
storage_key,
payload_hash,
last_commit_id,

 payload_size_bytes,screenshot_key,extinfo,status,version,created_at_ms,updated_at_ms)
 VALUES($1,
$2,
$3,
$4,
$5,
$6,
$7,
$8,
$13,
$9,
$10,
$11,
'active',
1,
$12,
$12)`,
			save.ID,
			save.UserID,
			save.Game.ID,
			save.Kind,
			save.Name,

			save.Slot, save.StorageKey, save.PayloadHash, save.SizeBytes, screenshot, info, now, save.LastCommitID)
	}
	tag,
		err := r.db.Exec(ctx,
		`UPDATE save_tab SET name=$4,
slot_key=$5,
storage_key=$6,
payload_hash=$7,
payload_size_bytes=$8,

 screenshot_key=$9,extinfo=$10,last_commit_id=$14,version=version+1,updated_at_ms=$11
 WHERE id=$1 AND user_id=$2 AND version=$3 AND status='active' AND game_id=$12 AND save_type=$13`,
		save.ID,
		save.UserID,
		save.Version,
		save.Name,
		save.Slot,
		save.StorageKey,
		save.PayloadHash,
		save.SizeBytes,
		screenshot,
		info,
		now,
		save.Game.ID,
		save.Kind,
		save.LastCommitID)
	if err != nil {
		return failure("overwrite save", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return nil
}

func (r *Repository) RenameSave(ctx context.Context, userID, id, name string, version, now int64) error {
	tag, err := r.db.Exec(ctx, `UPDATE save_tab SET name=$4,version=version+1,updated_at_ms=$5
 WHERE id=$1 AND user_id=$2 AND version=$3 AND status='active'`, id, userID, version, name, now)
	if err != nil {
		return failure("rename save", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return nil
}

func (r *Repository) DeleteSave(ctx context.Context, userID, id string, version, now int64) error {
	tag, err := r.db.Exec(ctx, `UPDATE save_tab SET status='deleted',version=version+1,updated_at_ms=$4
 WHERE id=$1 AND user_id=$2 AND version=$3 AND status='active'`, id, userID, version, now)
	if err != nil {
		return failure("delete save", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return nil
}
