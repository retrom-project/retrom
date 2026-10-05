package postgres

import "database/sql"

// arguments encodes the storage contract's checked 0/1 flags. Other values,
// including byte slices and millisecond int64 values, retain their native type.
func arguments(values []any) []any {
	var result []any
	for index, value := range values {
		var flag bool
		switch value := value.(type) {
		case bool:
			flag = value
		case *bool:
			if value == nil {
				continue
			}
			flag = *value
		case sql.NullBool:
			if !value.Valid {
				continue
			}
			flag = value.Bool
		default:
			continue
		}
		if result == nil {
			result = append([]any(nil), values...)
		}
		result[index] = int64(0)
		if flag {
			result[index] = int64(1)
		}
	}
	if result != nil {
		return result
	}
	return values
}
