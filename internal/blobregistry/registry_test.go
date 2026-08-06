package blobregistry

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/store"
)

func TestRegistryExactlyCoversBlobForeignKeys(t *testing.T) {
	t.Parallel()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if err := ValidateSchema(context.Background(), database.SQL); err != nil {
		t.Fatal(err)
	}
}
