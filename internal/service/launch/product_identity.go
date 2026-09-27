package launch

import (
	"fmt"

	"github.com/google/uuid"
)

func newProductID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("product identity: %w", err)
	}
	return id.String(), nil
}

func checkedProductID(next func() (string, error)) (string, error) {
	id, err := next()
	if err != nil {
		return "", fmt.Errorf("generate product identity: %w", err)
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 || parsed.String() != id {
		return "", ErrBlocked
	}
	return id, nil
}
