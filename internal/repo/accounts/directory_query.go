package accounts

import "retrom/internal/model/accounts"

type directorySQL struct {
	query     string
	arguments []any
}

func (builder *directorySQL) add(fragment string, values ...any) {
	builder.query += fragment
	builder.arguments = append(builder.arguments, values...)
}

func (builder *directorySQL) status(status string) {
	switch status {
	case "NON_DELETED":
		builder.add(` AND u.status!='DELETED'`)
	case "ENABLED", "DISABLED", "DELETED":
		builder.add(` AND u.status=?`, status)
	}
}

func (builder *directorySQL) sort(query accounts.UserQuery) {
	after := query.After
	switch query.Sort {
	case "CREATED_DESC":
		if after != nil {
			builder.add(` AND (u.created_at_ms<? OR (u.created_at_ms=? AND u.id<?))`, after.CreatedAt, after.CreatedAt, after.ID)
		}
		builder.add(` ORDER BY u.created_at_ms DESC,u.id DESC`)
	case "USERNAME_ASC":
		if after != nil {
			builder.add(` AND (u.username>? OR (u.username=? AND u.id>?))`, after.Username, after.Username, after.ID)
		}
		builder.add(` ORDER BY u.username ASC,u.id ASC`)
	case "LAST_LOGIN_DESC":
		builder.lastLogin(after)
	}
}

func (builder *directorySQL) lastLogin(after *accounts.UserCursor) {
	if after != nil {
		if after.NeverLoggedIn {
			builder.add(
				` AND u.last_login_at_ms IS NULL AND (u.created_at_ms<? OR (u.created_at_ms=? AND u.id<?))`,
				after.CreatedAt,
				after.CreatedAt,
				after.ID,
			)
		} else {
			builder.add(
				` AND (u.last_login_at_ms IS NULL OR u.last_login_at_ms<? OR (u.last_login_at_ms=? AND u.id<?))`,
				after.LastLogin,
				after.LastLogin,
				after.ID,
			)
		}
	}
	builder.add(` ORDER BY (u.last_login_at_ms IS NULL) ASC,u.last_login_at_ms DESC,
 CASE WHEN u.last_login_at_ms IS NULL THEN u.created_at_ms END DESC,u.id DESC`)
}
