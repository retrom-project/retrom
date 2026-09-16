package tagging

import (
	"fmt"

	model "retrom/internal/model/tagging"
)

// NormalizeName delegates to the model's pure name policy.
// Kept as a package-level function for backward compatibility with callers
// that import service/tagging for this symbol.
func NormalizeName(value string) (string, string, string, error) {
	name, key, search, err := model.NormalizeName(value)
	if err != nil {
		return "", "", "", fmt.Errorf("normalize name: %w", err)
	}
	return name, key, search, nil
}

// ValidID delegates to the model's pure ID validation.
func ValidID(value string) bool {
	return model.ValidID(value)
}

// ValidateIDs delegates to the model's pure ID validation.
func ValidateIDs(values []string) ([]string, error) {
	ids, err := model.ValidateIDs(values)
	if err != nil {
		return nil, fmt.Errorf("validate IDs: %w", err)
	}
	return ids, nil
}
