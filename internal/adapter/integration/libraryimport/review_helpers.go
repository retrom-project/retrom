package libraryimport

import (
	"database/sql"

	application "retrom/internal/service/libraryimport"
)

func parseArcadeDraftSnapshot(raw string) (arcadeDraftSnapshot, bool) {
	return application.ParseArcadeDraftSnapshot(raw)
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}
