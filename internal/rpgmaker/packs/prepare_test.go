package packs

import (
	"errors"
	"testing"

	"retrom/internal/importing"
)

func TestArchivePreparationErrorPreservesResourceAvailability(t *testing.T) {
	t.Parallel()

	unavailable := archivePreparationError("scan", importing.ErrArchiveResourceLimit)
	if !errors.Is(unavailable, ErrUnavailable) ||
		!errors.Is(unavailable, importing.ErrArchiveResourceLimit) ||
		errors.Is(unavailable, ErrInvalid) {
		t.Fatalf("resource error = %v", unavailable)
	}

	invalid := archivePreparationError("scan", importing.ErrArchiveUnsafe)
	if !errors.Is(invalid, ErrInvalid) ||
		!errors.Is(invalid, importing.ErrArchiveUnsafe) ||
		errors.Is(invalid, ErrUnavailable) {
		t.Fatalf("unsafe archive error = %v", invalid)
	}
}
