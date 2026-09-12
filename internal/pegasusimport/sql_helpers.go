package pegasusimport

import "database/sql"

func rowsAffected(result sql.Result) int64 {
	if result == nil {
		return 0
	}
	value, _ := result.RowsAffected()
	return value
}
