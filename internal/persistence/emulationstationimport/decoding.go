package emulationstationimport

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

func decodeArray[T any](value string, target *[]T) error {
	if err := json.Unmarshal([]byte(value), target); err != nil {
		return fmt.Errorf("decode stored array: %w", err)
	}
	if *target == nil {
		*target = []T{}
	}
	return nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
