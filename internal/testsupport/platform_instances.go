// Package testsupport contains explicit database fixtures shared by integration tests.
package testsupport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/platformcatalog"
	"retrom/internal/platforminstance"
	"retrom/internal/runtimecatalog"
	"retrom/internal/store"
)

type PlatformInstanceReference struct {
	ID          string
	Slug        string
	TemplateKey string
}

type PlatformInstanceReferences map[string]PlatformInstanceReference

// OpenDatabase opens a current-schema database and creates current catalog directories for tests whose
// subject is unrelated to recommended-directory initialization. Tests resolve identities through
// catalog_template_key because fresh databases intentionally contain no instance directory rows.
func OpenDatabase(ctx context.Context, path string, now func() time.Time) (*store.DB, error) {
	database, err := store.Open(ctx, path, now)
	if err != nil {
		return nil, fmt.Errorf("testsupport: open database: %w", err)
	}
	if _, err := BuildPlatformInstances(ctx, database.SQL); err != nil {
		return nil, errors.Join(err, database.Close())
	}
	return database, nil
}

// BuildPlatformInstances creates the current recommendation catalog with fresh identities and returns
// references keyed by catalog template key. Every invocation creates fresh UUIDv7 identities.
func BuildPlatformInstances(ctx context.Context, database *sql.DB) (PlatformInstanceReferences, error) {
	if err := SeedRuntimeProviders(ctx, database, currentRuntimeCatalog()); err != nil {
		return nil, err
	}
	references := make(PlatformInstanceReferences, len(platformcatalog.Current().Templates))
	for _, template := range platformcatalog.Current().Templates {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("testsupport: create platform instance id: %w", err)
		}
		slug, err := platforminstance.NextSlug(ctx, database, template.PlatformID, template.Name)
		if err != nil {
			return nil, fmt.Errorf("testsupport: create platform instance slug %s: %w", template.Key, err)
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO platform_instances(
  id,platform_id,default_core_id,name,slug,description,sort_order,enabled,version,
  created_at_ms,updated_at_ms,catalog_template_key
) VALUES(?,?,?,?,?,?,?,1,1,0,0,?)
`, id.String(), template.PlatformID, template.DefaultCoreID, template.Name, slug,
			template.Description, template.CatalogOrder, template.Key); err != nil {
			return nil, fmt.Errorf("testsupport: create platform instance %s: %w", template.Key, err)
		}
		references[template.Key] = PlatformInstanceReference{
			ID: id.String(), Slug: slug, TemplateKey: template.Key,
		}
	}
	return references, nil
}

// SeedPlatformInstances is a convenience for tests that only need a populated current catalog.
func SeedPlatformInstances(ctx context.Context, database *sql.DB) error {
	_, err := BuildPlatformInstances(ctx, database)
	return err
}

func currentRuntimeCatalog() runtimecatalog.Catalog {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("testsupport: locate runtime target catalog")
	}
	catalogPath := filepath.Join(
		filepath.Dir(filename), "..", "..", "data", "runtime-target-bindings", "v1", "catalog.json",
	)
	contents, err := os.ReadFile(catalogPath)
	if err != nil {
		panic(fmt.Sprintf("testsupport: read runtime target catalog: %v", err))
	}
	catalog, err := runtimecatalog.ParseCatalog(contents)
	if err != nil {
		panic(fmt.Sprintf("testsupport: parse runtime target catalog: %v", err))
	}
	return catalog
}

func PlatformInstanceID(ctx context.Context, database *sql.DB, templateKey string) (string, error) {
	var id string
	if err := database.QueryRowContext(ctx, `
SELECT id FROM platform_instances
WHERE catalog_template_key=? AND deleted_at_ms IS NULL
`, templateKey).Scan(&id); err != nil {
		return "", fmt.Errorf("testsupport: resolve platform instance %s: %w", templateKey, err)
	}
	return id, nil
}

func MustPlatformInstanceID(t testing.TB, database *sql.DB, templateKey string) string {
	t.Helper()
	id, err := PlatformInstanceID(t.Context(), database, templateKey)
	if err != nil {
		t.Fatalf("resolve platform instance fixture: %v", err)
	}
	return id
}
