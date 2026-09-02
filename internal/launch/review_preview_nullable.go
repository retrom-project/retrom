package launch

import "database/sql"

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTextPointer(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableSQLString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}
