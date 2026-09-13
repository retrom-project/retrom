package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/contentquery"
	application "retrom/internal/service/catalog"
)

type Repository struct{ database *sql.DB }

func New(database *sql.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) Platforms(ctx context.Context) ([]application.PlatformRow, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT p.id,p.name,p.sort_order,p.enabled,pc.core_id,c.name,pc.enabled,
binding.provider_id,binding.target_id
FROM platforms p
LEFT JOIN platform_cores pc ON pc.platform_id=p.id
LEFT JOIN cores c ON c.id=pc.core_id
LEFT JOIN runtime_binding_platforms binding_platform
 ON binding_platform.platform_id=p.id AND binding_platform.core_id=pc.core_id
LEFT JOIN runtime_target_bindings binding ON binding.binding_id=binding_platform.binding_id
 AND binding.launch_policy!='DISABLED'
LEFT JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
ORDER BY p.sort_order,pc.core_id`)
	if err != nil {
		return nil, fmt.Errorf("query platforms: %w", err)
	}
	defer func() { cleanup.Error("close platforms", rows.Close()) }()
	result := make([]application.PlatformRow, 0)
	for rows.Next() {
		var row application.PlatformRow
		var coreID, coreName, providerID, targetID sql.NullString
		var coreEnabled sql.NullInt64
		if err := rows.Scan(&row.ID, &row.Name, &row.SortOrder, &row.Enabled,
			&coreID, &coreName, &coreEnabled, &providerID, &targetID); err != nil {
			return nil, fmt.Errorf("scan platform: %w", err)
		}
		if coreID.Valid {
			row.CoreID = &coreID.String
		}
		if coreName.Valid {
			row.CoreName = &coreName.String
		}
		if coreEnabled.Valid {
			value := coreEnabled.Int64 == 1
			row.CoreEnabled = &value
		}
		if providerID.Valid {
			row.ProviderID = &providerID.String
		}
		if targetID.Valid {
			row.TargetID = &targetID.String
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate platforms: %w", err)
	}
	return result, nil
}

func (repository *Repository) RuntimeTargets(ctx context.Context) ([]application.RuntimeTarget, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT provider.provider_id,provider.provider_version,provider.provider_api_version,
provider.bundle_sha256,target.target_id,target.display_name,binding.core_id,c.name,binding.launch_policy
FROM runtime_providers provider
JOIN runtime_targets target ON target.provider_id=provider.provider_id
JOIN runtime_target_bindings binding ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
JOIN cores c ON c.id=binding.core_id
ORDER BY provider.provider_id,target.target_id`)
	if err != nil {
		return nil, fmt.Errorf("query runtime targets: %w", err)
	}
	defer func() { cleanup.Error("close runtime targets", rows.Close()) }()
	result := make([]application.RuntimeTarget, 0)
	for rows.Next() {
		var item application.RuntimeTarget
		if err := rows.Scan(&item.ProviderID, &item.ProviderVersion, &item.ProviderAPIVersion,
			&item.BundleSHA256, &item.TargetID, &item.DisplayName, &item.CoreID,
			&item.CoreName, &item.LaunchPolicy); err != nil {
			return nil, fmt.Errorf("scan runtime target: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime targets: %w", err)
	}
	return result, nil
}

func (repository *Repository) PlatformInstances(
	ctx context.Context,
	query application.PlatformInstanceQuery,
) ([]application.PlatformInstance, error) {
	conditions := []string{"pi.deleted_at_ms IS NULL"}
	arguments := make([]any, 0, 2)
	if query.PlatformID != nil {
		conditions = append(conditions, "pi.platform_id=?")
		arguments = append(arguments, *query.PlatformID)
	}
	if query.Enabled != nil {
		conditions = append(conditions, "pi.enabled=?")
		value := 0
		if *query.Enabled {
			value = 1
		}
		arguments = append(arguments, value)
	}
	statement := `
SELECT pi.id,pi.platform_id,p.name,pi.default_core_id,c.name,pi.name,pi.slug,pi.description,
pi.sort_order,pi.enabled,pi.version,pi.updated_at_ms,
(SELECT count(*) FROM games g WHERE g.platform_instance_id=pi.id),
COALESCE((SELECT ` + contentquery.BindingPolicySQL + `
 FROM runtime_target_bindings binding
 JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
  AND binding_platform.platform_id=pi.platform_id AND binding_platform.core_id=pi.default_core_id
 WHERE binding.core_id=pi.default_core_id AND binding.launch_policy<>'DISABLED'
 LIMIT 1),NULL)
FROM platform_instances pi
JOIN platforms p ON p.id=pi.platform_id
JOIN cores c ON c.id=pi.default_core_id
WHERE ` + strings.Join(conditions, " AND ") + ` ORDER BY pi.sort_order,pi.id LIMIT 100`
	rows, err := repository.database.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query platform instances: %w", err)
	}
	defer func() { cleanup.Error("close platform instances", rows.Close()) }()
	result := make([]application.PlatformInstance, 0)
	for rows.Next() {
		var item application.PlatformInstance
		var enabled int
		var policy contentcapability.Policy
		if err := rows.Scan(&item.ID, &item.PlatformID, &item.PlatformName, &item.DefaultCoreID,
			&item.DefaultCoreName, &item.Name, &item.Slug, &item.Description, &item.SortOrder,
			&enabled, &item.Version, &item.UpdatedAtMS, &item.GameCount,
			contentquery.ScanPolicy(&policy)); err != nil {
			return nil, fmt.Errorf("scan platform instance: %w", err)
		}
		item.Enabled = enabled == 1
		item.ContentPolicy = policy
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate platform instances: %w", err)
	}
	return result, nil
}

var _ application.Repository = (*Repository)(nil)
