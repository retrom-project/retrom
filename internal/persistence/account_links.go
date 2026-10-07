package persistence

import (
	"context"

	"retrom/internal/model"
)

type LinkRecord struct {
	Link       model.AccountLink
	ConsumedAt *int64
	RevokedAt  *int64
}

func (r *Repository) CreateLink(ctx context.Context, link model.AccountLink, createdBy string, now int64) error {
	if link.Kind == "password_reset" {
		if err := r.Execute(ctx, `UPDATE account_link_tab SET revoked_at_ms=$2,revoked_by_kind='superseded',version=version+1
 WHERE target_user_id=$1 AND kind='password_reset' AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL`,
			link.TargetUserID, now); err != nil {
			return err
		}
	}
	return r.Execute(ctx, `INSERT INTO account_link_tab
 (id,kind,invited_role,target_user_id,created_by_user_id,expires_at_ms,version,created_at_ms)
 VALUES($1,
$2,
$3,
NULLIF($4,
''),
$5,
$6,
1,
$7)`,
		link.ID,
		link.Kind,
		link.Role,
		link.TargetUserID,
		createdBy,
		link.ExpiresAtMs,
		now)
}

func (r *Repository) Link(ctx context.Context, id string, lock bool) (LinkRecord, error) {
	record := LinkRecord{}
	query := `SELECT id,kind,COALESCE(invited_role,''),COALESCE(target_user_id,''),expires_at_ms,
 version,consumed_at_ms,revoked_at_ms FROM account_link_tab WHERE id=$1`
	if lock {
		query += " FOR UPDATE"
	}
	l := &record.Link
	err := r.db.QueryRow(ctx, query, id).Scan(&l.ID, &l.Kind, &l.Role, &l.TargetUserID, &l.ExpiresAtMs, &l.Version,
		&record.ConsumedAt, &record.RevokedAt)
	if err != nil {
		return record, failure("read account link", err)
	}
	return record, nil
}

func (r *Repository) ConsumeLink(ctx context.Context, id, userID string, now int64) error {
	tag, err := r.db.Exec(ctx, `UPDATE account_link_tab SET consumed_at_ms=$3,consumed_by_user_id=$2,version=version+1
 WHERE id=$1 AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>$3`, id, userID, now)
	if err != nil {
		return failure("consume account link", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrNotFound
	}
	return nil
}

func (r *Repository) RevokeLink(ctx context.Context, id, userID string, version, now int64) error {
	tag, err := r.db.Exec(ctx, `UPDATE account_link_tab SET revoked_at_ms=$4,revoked_by_kind='admin',
 revoked_by_user_id=$2,version=version+1 WHERE id=$1 AND version=$3 AND consumed_at_ms IS NULL AND revoked_at_ms
IS NULL`, id, userID, version, now)
	if err != nil {
		return failure("revoke account link", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return nil
}

func (r *Repository) Links(ctx context.Context, q model.Query,
	now int64,
) (model.Page[model.AccountLinkSummary], error) {
	page := model.Page[model.AccountLinkSummary]{Offset: q.Offset, Limit: q.Limit}
	const projection = `SELECT id,kind,invited_role,expires_at_ms,created_at_ms,version,
 CASE WHEN consumed_at_ms IS NOT NULL THEN 'consumed' WHEN revoked_at_ms IS NOT NULL THEN 'revoked'
 WHEN expires_at_ms<=$1 THEN 'expired' ELSE 'active' END AS status FROM account_link_tab`
	items, err := listJSON[model.AccountLinkSummary](ctx, r, `WITH links AS (`+projection+`)
 SELECT jsonb_build_object('id',id,'kind',kind,'role',nullif(invited_role,''),'expiresAtMs',expires_at_ms,
 'createdAtMs',created_at_ms,'version',version,'status',status) FROM links WHERE ($2='' OR status=$2)
 ORDER BY created_at_ms DESC,id LIMIT $3 OFFSET $4`, now, q.Status, q.Limit, q.Offset)
	if err != nil {
		return page, err
	}
	page.Items = items
	page.Total, err = r.Count(ctx,
		`WITH links AS (`+projection+`) SELECT count(*) FROM links WHERE ($2='' OR status=$2)`, now, q.Status)
	return page, err
}
