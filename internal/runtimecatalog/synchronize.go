package runtimecatalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// SynchronizeDefinitions projects validated Host declarations in the caller's startup transaction.
// It never updates user-created platform instances, defaults or installations.
func SynchronizeDefinitions(ctx context.Context, transaction *sql.Tx, catalog Catalog, now int64) error {
	if err := pruneUnreferencedDefinitions(ctx, transaction, catalog.Definitions); err != nil {
		return err
	}
	for _, platform := range catalog.Definitions.Platforms {
		_, err := transaction.ExecContext(ctx, `
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES(?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name,sort_order=excluded.sort_order,
enabled=excluded.enabled,updated_at_ms=excluded.updated_at_ms
`, platform.ID, platform.Name, platform.SortOrder, platform.Enabled, now, now)
		if err != nil {
			return fmt.Errorf("reconcile product platform: %w", err)
		}
	}
	for _, core := range catalog.Definitions.Cores {
		_, err := transaction.ExecContext(ctx, `
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES(?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name,enabled=excluded.enabled,updated_at_ms=excluded.updated_at_ms
`, core.ID, core.Name, core.Enabled, now, now)
		if err != nil {
			return fmt.Errorf("reconcile product core: %w", err)
		}
	}
	for _, kind := range catalog.Definitions.ContentKinds {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO content_kinds(id) VALUES(?) ON CONFLICT(id) DO NOTHING
`, kind); err != nil {
			return fmt.Errorf("reconcile product content kind: %w", err)
		}
	}
	if err := writePackDefinitions(ctx, transaction, catalog, now); err != nil {
		return err
	}
	return writeProductRelations(ctx, transaction, catalog)
}

func writePackDefinitions(ctx context.Context, transaction *sql.Tx, catalog Catalog, now int64) error {
	for _, pack := range catalog.Definitions.AssetPacks {
		result, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_asset_pack_definitions(id,kind,generation,declared_name,normalized_declared_name,
display_name,required_layout_version,origin,enabled,created_by_user_id,created_at_ms)
VALUES(?,?,?,?,?,?,?,'BUILTIN',?,NULL,?)
ON CONFLICT(id) DO UPDATE SET kind=excluded.kind,generation=excluded.generation,
declared_name=excluded.declared_name,normalized_declared_name=excluded.normalized_declared_name,
display_name=excluded.display_name,required_layout_version=excluded.required_layout_version,enabled=excluded.enabled
WHERE runtime_asset_pack_definitions.origin='BUILTIN' AND (
 NOT EXISTS(SELECT 1 FROM runtime_asset_pack_installations installed WHERE installed.definition_id=excluded.id)
 OR runtime_asset_pack_definitions.kind=excluded.kind
 AND runtime_asset_pack_definitions.generation=excluded.generation
 AND runtime_asset_pack_definitions.normalized_declared_name=excluded.normalized_declared_name
 AND runtime_asset_pack_definitions.required_layout_version=excluded.required_layout_version
)
`, pack.ID, pack.Kind, pack.Generation, pack.DeclaredName, pack.NormalizedDeclaredName, pack.DisplayName,
			pack.RequiredLayoutVersion, pack.Enabled, now)
		if err != nil {
			return fmt.Errorf("reconcile product asset pack: %w", err)
		}
		if count, err := result.RowsAffected(); err != nil || count != 1 {
			return fmt.Errorf("reconcile product asset pack: %w", ErrCatalogInvalid)
		}
	}
	return nil
}

func writeProductRelations(ctx context.Context, transaction *sql.Tx, catalog Catalog) error {
	bindings, err := json.Marshal(catalog.Bindings)
	if err != nil {
		return fmt.Errorf("encode product relations: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE platform_cores SET enabled=0 WHERE enabled=1 AND NOT EXISTS(
 SELECT 1 FROM json_each(?) binding,json_each(binding.value,'$.platformIds') platform
 WHERE json_extract(binding.value,'$.coreId')=platform_cores.core_id AND platform.value=platform_cores.platform_id
)
`, string(bindings)); err != nil {
		return fmt.Errorf("disable omitted product relations: %w", err)
	}
	for _, binding := range catalog.Bindings {
		for _, platformID := range binding.PlatformIDs {
			_, err := transaction.ExecContext(ctx, `
INSERT INTO platform_cores(platform_id,core_id,enabled)
SELECT platform.id,core.id,platform.enabled AND core.enabled FROM platforms platform,cores core
WHERE platform.id=? AND core.id=?
ON CONFLICT(platform_id,core_id) DO UPDATE SET enabled=excluded.enabled
`, platformID, binding.CoreID)
			if err != nil {
				return fmt.Errorf("reconcile product platform/core: %w", err)
			}
		}
	}
	return nil
}

// Foreign keys reject removal of referenced definitions; no product table has
// cascading deletion into user-owned data. The caller rolls back the entire
// projection (including provider activation and audit) on any failure.
func pruneUnreferencedDefinitions(ctx context.Context, transaction *sql.Tx, definitions Definitions) error {
	encoded, err := json.Marshal(definitions)
	if err != nil {
		return fmt.Errorf("encode product definitions: %w", err)
	}
	statements := []string{
		`DELETE FROM platform_cores WHERE
   NOT EXISTS(SELECT 1 FROM json_each(?1,'$.platforms') declared
    WHERE json_extract(declared.value,'$.id')=platform_cores.platform_id)
   OR NOT EXISTS(SELECT 1 FROM json_each(?1,'$.cores') declared
    WHERE json_extract(declared.value,'$.id')=platform_cores.core_id)`,
		`DELETE FROM cores WHERE NOT EXISTS(SELECT 1 FROM json_each(?1,'$.cores') declared
    WHERE json_extract(declared.value,'$.id')=cores.id)`,
		`DELETE FROM platforms WHERE NOT EXISTS(SELECT 1 FROM json_each(?1,'$.platforms') declared
    WHERE json_extract(declared.value,'$.id')=platforms.id)`,
		`DELETE FROM content_kinds WHERE NOT EXISTS(SELECT 1 FROM json_each(?1,'$.contentKinds') declared
    WHERE declared.value=content_kinds.id)`,
		`DELETE FROM runtime_asset_pack_definitions WHERE origin='BUILTIN' AND NOT EXISTS(
   SELECT 1 FROM json_each(?1,'$.assetPacks') declared
    WHERE json_extract(declared.value,'$.id')=runtime_asset_pack_definitions.id)`,
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement, string(encoded)); err != nil {
			return fmt.Errorf("%w: referenced product definition cannot be removed: %w", ErrCatalogInvalid, err)
		}
	}
	return nil
}
