package favorites

import "github.com/google/uuid"

func ValidID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}
