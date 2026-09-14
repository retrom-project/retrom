package tagging

import "github.com/google/uuid"

func ValidID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value && parsed.Version() == 7
}

func ValidateIDs(values []string) ([]string, error) {
	if values == nil || len(values) > MaxTagsPerOwner {
		if len(values) > MaxTagsPerOwner {
			return nil, ErrAssignmentLimitExceeded
		}
		return nil, ErrInvalid
	}
	result := append([]string{}, values...)
	seen := make(map[string]struct{}, len(result))
	for _, value := range result {
		if !ValidID(value) {
			return nil, ErrInvalid
		}
		if _, exists := seen[value]; exists {
			return nil, ErrInvalid
		}
		seen[value] = struct{}{}
	}
	return result, nil
}
