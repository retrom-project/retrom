package blobregistry

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/store"
	"retrom/internal/testassert"
)

func TestRegistryExactlyCoversBlobForeignKeys(t *testing.T) {
	t.Parallel()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if err := ValidateSchema(context.Background(), database.SQL); err != nil {
		t.Fatal(err)
	}
}
