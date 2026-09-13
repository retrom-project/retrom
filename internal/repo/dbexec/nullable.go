package dbexec

import "database/sql"

// StringPointer converts an optional SQL string at the persistence boundary.
func StringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
