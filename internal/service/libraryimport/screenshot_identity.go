package libraryimport

import (
	"fmt"

	"github.com/google/uuid"
)

func newScreenshotID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("screenshot identity: %w", err)
	}
	return id.String(), nil
}

func checkedScreenshotID(next func() (string, error)) (string, error) {
	id, err := next()
	if err != nil {
		return "", fmt.Errorf("generate screenshot identity: %w", err)
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 || parsed.String() != id {
		return "", ErrReviewScreenshotInvalid
	}
	return id, nil
}
