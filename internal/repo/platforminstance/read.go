package platforminstance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/model/platforminstance"
	"retrom/internal/repo/contentquery"
	"retrom/internal/repo/dbexec"

	"retrom/internal/capability/runtime/platformcatalog"
)

func (reader records) CatalogReferences(
	ctx context.Context,
	catalog platformcatalog.Catalog,
) (map[string]platforminstance.CatalogReference, error) {
	result := make(map[string]platforminstance.CatalogReference, len(catalog.Templates))
	for _, template := range catalog.Templates {
		var reference platforminstance.CatalogReference
		err := reader.database.QueryRowContext(ctx, `
SELECT p.name,c.name
FROM platforms p
JOIN platform_cores pc ON pc.platform_id=p.id AND pc.core_id=? AND pc.enabled=1
JOIN cores c ON c.id=pc.core_id AND c.enabled=1
WHERE p.id=? AND p.enabled=1
AND (
  SELECT count(*)
  FROM runtime_target_bindings binding
  JOIN runtime_binding_platforms binding_platform
    ON binding_platform.binding_id=binding.binding_id
   AND binding_platform.platform_id=p.id AND binding_platform.core_id=c.id
  WHERE binding.core_id=c.id AND binding.launch_policy<>'DISABLED'
) = CASE WHEN c.id='rpgmaker' THEN 7 ELSE 1 END
`, template.DefaultCoreID, template.PlatformID).
			Scan(&reference.PlatformName, &reference.CoreName)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: unresolved template %s", platforminstance.ErrCatalogInvalid, template.Key)
		}
		if err != nil {
			return nil, fmt.Errorf("platforminstance: validate template %s: %w", template.Key, err)
		}
		result[template.Key] = reference
	}
	return result, nil
}

func (reader records) Directories(ctx context.Context) ([]platforminstance.Directory, error) {
	rows, err := reader.database.QueryContext(ctx, `
SELECT id,platform_id,default_core_id,name,description,sort_order,enabled,version,catalog_template_key,deleted_at_ms
FROM platform_instances
ORDER BY sort_order,id
`)
	if err != nil {
		return nil, fmt.Errorf("platforminstance: query directories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]platforminstance.Directory, 0)
	for rows.Next() {
		var row platforminstance.Directory
		var enabled int
		var key sql.NullString
		var deleted sql.NullInt64
		if err := rows.Scan(&row.ID, &row.PlatformID, &row.CoreID, &row.Name, &row.Description,
			&row.SortOrder, &enabled, &row.Version, &key, &deleted); err != nil {
			return nil, fmt.Errorf("platforminstance: scan directory: %w", err)
		}
		row.Enabled = enabled == 1
		if key.Valid {
			row.CatalogKey = &key.String
		}
		row.Deleted = deleted.Valid
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("platforminstance: iterate directories: %w", err)
	}
	return result, nil
}

func (reader records) Instance(ctx context.Context, id string) (platforminstance.Instance, error) {
	var instance platforminstance.Instance
	var enabled int
	contentPolicy := instance.ContentPolicy
	err := reader.database.QueryRowContext(ctx, `
SELECT pi.id,pi.platform_id,p.name,pi.default_core_id,c.name,pi.name,pi.slug,pi.description,
pi.sort_order,pi.enabled,pi.version,pi.created_at_ms,pi.updated_at_ms,
(SELECT count(*) FROM games g WHERE g.platform_instance_id=pi.id),
COALESCE((SELECT `+contentquery.BindingPolicySQL+`
 FROM runtime_target_bindings binding
 JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
  AND binding_platform.platform_id=pi.platform_id AND binding_platform.core_id=pi.default_core_id
 WHERE binding.core_id=pi.default_core_id AND binding.launch_policy<>'DISABLED'
 LIMIT 1),NULL)
FROM platform_instances pi
JOIN platforms p ON p.id=pi.platform_id
JOIN cores c ON c.id=pi.default_core_id
WHERE pi.id=? AND pi.deleted_at_ms IS NULL
`, id).Scan(
		&instance.ID, &instance.PlatformID, &instance.PlatformName, &instance.DefaultCoreID,
		&instance.DefaultCoreName, &instance.Name, &instance.Slug, &instance.Description,
		&instance.SortOrder, &enabled, &instance.Version, &instance.CreatedAtMS, &instance.UpdatedAtMS,
		&instance.GameCount, contentquery.ScanPolicy(&contentPolicy),
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return platforminstance.Instance{}, fmt.Errorf("%w: %w", platforminstance.ErrNotFound, err)
		}
		return platforminstance.Instance{}, fmt.Errorf("platforminstance: read created directory: %w", err)
	}
	instance.Enabled = enabled == 1
	instance.ContentPolicy = contentPolicy
	return instance, nil
}

func (reader records) CoreImpact(
	ctx context.Context, instanceID, coreID string, expected int64,
) (platforminstance.CoreImpactFacts, error) {
	var facts platforminstance.CoreImpactFacts
	var enabled int
	err := reader.database.QueryRowContext(ctx, `
SELECT platform_id,version,enabled
FROM platform_instances
WHERE id=? AND deleted_at_ms IS NULL
`, instanceID).Scan(&facts.PlatformID, &facts.PlatformInstanceVersion, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return platforminstance.CoreImpactFacts{}, fmt.Errorf(
			"%w: platform instance changed", platforminstance.ErrImpactStale,
		)
	}
	if err != nil {
		return platforminstance.CoreImpactFacts{}, fmt.Errorf("platforminstance: read impact instance: %w", err)
	}
	if facts.PlatformInstanceVersion != expected {
		return platforminstance.CoreImpactFacts{}, fmt.Errorf(
			"%w: platform instance changed", platforminstance.ErrImpactStale,
		)
	}
	if enabled != 1 {
		return platforminstance.CoreImpactFacts{}, fmt.Errorf(
			"%w: platform instance disabled", platforminstance.ErrInvalidCore,
		)
	}
	var allowed int
	if err := reader.database.QueryRowContext(ctx, `
SELECT count(*)
FROM platform_cores
WHERE platform_id=? AND core_id=? AND enabled=1
`, facts.PlatformID, coreID).Scan(&allowed); err != nil {
		return platforminstance.CoreImpactFacts{}, fmt.Errorf("platforminstance: validate impact core: %w", err)
	}
	if allowed != 1 {
		return platforminstance.CoreImpactFacts{}, fmt.Errorf(
			"%w: core is not enabled for platform", platforminstance.ErrInvalidCore,
		)
	}
	var datVersionID sql.NullString
	if err := reader.database.QueryRowContext(ctx, `
SELECT binding.provider_id,binding.target_id,provider.bundle_sha256,
(SELECT id
 FROM dat_versions
 WHERE provider_id=binding.provider_id AND target_id=binding.target_id AND is_active=1)
FROM runtime_target_bindings binding
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=? AND binding_platform.core_id=?
JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
JOIN runtime_providers provider ON provider.provider_id=target.provider_id
WHERE binding.core_id=? AND binding.launch_policy<>'DISABLED'
	`, facts.PlatformID, coreID, coreID).Scan(
		&facts.ProviderID, &facts.TargetID, &facts.BundleSHA256, &datVersionID,
	); err != nil {
		return platforminstance.CoreImpactFacts{}, fmt.Errorf(
			"%w: runtime target binding unavailable", platforminstance.ErrInvalidCore,
		)
	}
	facts.DATVersionID = dbexec.StringPointer(datVersionID)
	rows, err := reader.database.QueryContext(ctx, `
SELECT g.id,g.version,variant.id,variant.status,variant.compatibility_code
FROM games g
LEFT JOIN game_variants variant ON variant.game_id=g.id AND variant.core_id=?
WHERE g.platform_instance_id=? AND g.status='PUBLISHED'
ORDER BY g.id
`, coreID, instanceID)
	if err != nil {
		return platforminstance.CoreImpactFacts{}, fmt.Errorf("platforminstance: query impact games: %w", err)
	}
	defer func() { _ = rows.Close() }()
	facts.Games = make([]platforminstance.CoreImpactGame, 0)
	for rows.Next() {
		var game platforminstance.CoreImpactGame
		var variantID, status, compatibilityCode sql.NullString
		if err := rows.Scan(&game.GameID, &game.GameVersion, &variantID, &status, &compatibilityCode); err != nil {
			return platforminstance.CoreImpactFacts{}, fmt.Errorf("platforminstance: scan impact game: %w", err)
		}
		game.VariantID = dbexec.StringPointer(variantID)
		game.VariantStatus = dbexec.StringPointer(status)
		game.TargetCompatibilityCode = dbexec.StringPointer(compatibilityCode)
		facts.Games = append(facts.Games, game)
	}
	if err := rows.Err(); err != nil {
		return platforminstance.CoreImpactFacts{}, fmt.Errorf("platforminstance: iterate impact games: %w", err)
	}
	return facts, nil
}

func (reader records) UsedSlugs(ctx context.Context, platformID, base string) ([]string, error) {
	prefix := base + "-"
	rows, err := reader.database.QueryContext(ctx, `
SELECT slug FROM platform_instances
WHERE platform_id=? AND (slug=? OR substr(slug,1,?)=?)
`, platformID, base, len(prefix), prefix)
	if err != nil {
		return nil, fmt.Errorf("platforminstance: query slugs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	used := make([]string, 0)
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, fmt.Errorf("platforminstance: scan slug: %w", err)
		}
		used = append(used, slug)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("platforminstance: iterate slugs: %w", err)
	}
	return used, nil
}

func (reader records) CoreEnabled(ctx context.Context, platformID, coreID string) (bool, error) {
	var count int
	if err := reader.database.QueryRowContext(ctx, `
SELECT count(*) FROM platform_cores WHERE platform_id=? AND core_id=? AND enabled=1
`, platformID, coreID).Scan(&count); err != nil {
		return false, fmt.Errorf("platforminstance: validate default core: %w", err)
	}
	return count == 1, nil
}
