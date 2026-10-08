package directory

import (
	"errors"
	"testing"

	"retrom/internal/model"
)

func TestNulDescriptionRejectedBeforeCatalogOrPersistence(t *testing.T) {
	t.Parallel()
	s := &Service{}
	input := model.DirectoryInput{Name: "Directory", Slug: "directory", Description: "a\x00b"}
	if err := s.Validate(t.Context(), input); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("NUL description accepted: %v", err)
	}
}
