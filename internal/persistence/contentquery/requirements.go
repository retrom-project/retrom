package contentquery

import (
	"context"
	"fmt"

	"retrom/internal/content/requirements"
	dbapi "retrom/internal/database"
)

// LoadRequirements reads the immutable catalog only for content validation.
// List projections and transactional policy fences carry just its digest.
func LoadRequirements(ctx context.Context, executor dbapi.Executor, policy *requirements.Policy) error {
	if policy == nil || policy.Kind != requirements.FlycastCartridge {
		return nil
	}
	var contents []byte
	if err := dbapi.QueryRowContext(ctx, executor,
		`SELECT document_json FROM runtime_requirement_catalogs WHERE sha256=?`, policy.Catalog.SHA256,
	).Scan(&contents); err != nil {
		return fmt.Errorf("read target content catalog: %w", err)
	}
	catalog, err := requirements.ParseFlycastCatalog(contents, policy)
	if err != nil {
		return fmt.Errorf("parse target content catalog: %w", err)
	}
	policy.CatalogFacts = catalog
	return nil
}
