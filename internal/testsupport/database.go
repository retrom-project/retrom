package testsupport

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"retrom/internal/persistence"
)

func Database(t *testing.T) *persistence.Repository {
	t.Helper()
	address := os.Getenv("RETROM_TEST_DATABASE_URL")
	if address == "" {
		t.Fatal("RETROM_TEST_DATABASE_URL is required for integration tests")
	}
	ctx := t.Context()
	administrator, err := pgx.Connect(ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	name := "retrom_test_" + uuid.New().String()
	if _, err = administrator.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	repository, err := persistence.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		repository.Close()
		cleanupCtx := context.WithoutCancel(ctx)
		if _,
			dropErr := administrator.Exec(cleanupCtx,
			"DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); dropErr != nil {
			t.Error(fmt.Errorf("drop test database: %w", dropErr))
		}
		if closeErr := administrator.Close(cleanupCtx); closeErr != nil {
			t.Error(closeErr)
		}
	})
	if err = repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return repository
}
