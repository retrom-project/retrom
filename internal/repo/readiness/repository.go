package readiness

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/readiness"
)

type Repository struct{ database *sql.DB }

func New(database *sql.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) Check(ctx context.Context) (application.Status, error) {
	if err := repository.database.PingContext(ctx); err != nil {
		return application.Status{}, fmt.Errorf("ping readiness database: %w", err)
	}
	var missing int64
	if err := repository.database.QueryRowContext(ctx, `
SELECT count(*)
FROM runtime_target_bindings binding
WHERE binding.launch_policy<>'DISABLED'
AND binding.core_id IN ('fbneo','mame2003','mame2003_plus')
AND NOT EXISTS(
 SELECT 1 FROM dat_versions d
 WHERE d.provider_id=binding.provider_id AND d.target_id=binding.target_id
 AND d.is_active=1 AND d.parse_status='READY'
)`).Scan(&missing); err != nil {
		return application.Status{}, fmt.Errorf("query missing readiness dependencies: %w", err)
	}
	if missing == 0 {
		return application.Status{}, nil
	}
	var failed int64
	if err := repository.database.QueryRowContext(ctx, `
SELECT count(*)
FROM runtime_target_bindings binding
WHERE binding.launch_policy<>'DISABLED'
AND binding.core_id IN ('fbneo','mame2003','mame2003_plus')
AND NOT EXISTS(
 SELECT 1 FROM dat_versions active
 WHERE active.provider_id=binding.provider_id AND active.target_id=binding.target_id
 AND active.is_active=1 AND active.parse_status='READY'
)
AND EXISTS(
 SELECT 1 FROM dat_versions failed
 WHERE failed.provider_id=binding.provider_id AND failed.target_id=binding.target_id
 AND failed.parse_status='FAILED'
)`).Scan(&failed); err != nil {
		return application.Status{}, fmt.Errorf("query failed readiness dependencies: %w", err)
	}
	return application.Status{Missing: missing, Failed: failed}, nil
}

var _ application.Repository = (*Repository)(nil)
