package maintenance

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestDatabaseValidationPreservesCancellation(t *testing.T) {
	database, err := openDatabase(t.Context(), filepath.Join(t.TempDir(), "retrom.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := checkDatabase(ctx, database); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled integrity check lost cancellation cause: %v", err)
	}
}
