package accounts

import "retrom/internal/model/accounts"

func buildLinkListQuery(filter accounts.LinkListFilter, now int64) (string, []any) {
	query := accountLinkProjection + ` WHERE link.kind=?`
	arguments := []any{filter.Kind}
	if filter.TargetUserID != "" {
		query += " AND link.target_user_id=?"
		arguments = append(arguments, filter.TargetUserID)
	}
	switch filter.State {
	case "ACTIVE":
		query += " AND link.consumed_at_ms IS NULL AND link.revoked_at_ms IS NULL AND link.expires_at_ms>?"
		arguments = append(arguments, now)
	case "CONSUMED":
		query += " AND link.consumed_at_ms IS NOT NULL"
	case "REVOKED":
		query += " AND link.consumed_at_ms IS NULL AND link.revoked_at_ms IS NOT NULL"
	case "EXPIRED":
		query += ` AND link.consumed_at_ms IS NULL AND link.revoked_at_ms IS NULL
AND link.expires_at_ms<=?`
		arguments = append(arguments, now)
	}
	if filter.AfterID != "" {
		query += " AND (link.created_at_ms<? OR (link.created_at_ms=? AND link.id<?))"
		arguments = append(arguments, filter.AfterAtMS, filter.AfterAtMS, filter.AfterID)
	}
	query += " ORDER BY link.created_at_ms DESC,link.id DESC LIMIT ?"
	arguments = append(arguments, filter.Limit)
	return query, arguments
}
