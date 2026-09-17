package runtimeprovider

import (
	"context"
	"fmt"

	service "retrom/internal/model/runtimeprovider"
	"retrom/internal/repo/dbexec"

	runtimecatalogpersistence "retrom/internal/repo/runtimecatalog"

	"retrom/internal/repo/recordstore"

	"retrom/internal/capability/runtime/runtimecatalog"
)

func terminateProviderSessions(ctx context.Context, transaction dbexec.Executor, providerID string, now int64) error {
	if _, err := recordstore.UpdateNetplayRooms(ctx, transaction, recordstore.Update{
		Set: `
state='ENDED',current_session_id=NULL,ended_at_ms=?,end_reason='SERVER_RESTARTED',
updated_at_ms=?,version=version+1
`,
		Scope: recordstore.Scope{
			Where: `
state IN ('WAITING','STARTING','RUNNING') AND current_session_id IN (
 SELECT id FROM netplay_sessions WHERE provider_id=?
)
`,
			Args: []any{providerID},
		},
		Values: []any{now, now},
	}); err != nil {
		return fmt.Errorf("reconcile runtime providers: terminate sessions: %w", err)
	}
	if _, err := recordstore.UpdateNetplaySessions(ctx, transaction, recordstore.Update{
		Set: `
state='FAILED',end_reason='SERVER_RESTARTED',
finished_at_ms=?,updated_at_ms=?,version=version+1
`,
		Scope: recordstore.Scope{
			Where: `provider_id=? AND state NOT IN ('FINISHED','FAILED')`,
			Args:  []any{providerID},
		},
		Values: []any{now, now},
	}); err != nil {
		return fmt.Errorf("reconcile runtime providers: terminate sessions: %w", err)
	}
	return nil
}

func clearHostBindings(ctx context.Context, transaction dbexec.Executor) error {
	tables := []struct {
		name  string
		label string
	}{
		{"runtime_binding_content_kinds", "binding content kinds"},
		{"runtime_binding_platforms", "binding platforms"},
		{"runtime_target_bindings", "host bindings"},
	}
	for _, table := range tables {
		query := "DELETE FROM " + table.name
		if _, err := transaction.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("reconcile runtime providers: clear %s: %w", table.label, err)
		}
	}
	return nil
}

func writeProvider(
	ctx context.Context,
	transaction dbexec.Executor,
	provider service.ProviderProjection,
	now int64,
) error {
	var repository, tag, commit any
	if provider.Release != nil {
		repository, tag, commit = provider.Release.Repository, provider.Release.Tag, provider.Release.Commit
	}
	_, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_providers(
  provider_id,provider_version,provider_api_version,bundle_sha256,manifest_sha256,module_sha256,
  source,release_repository,release_tag,release_commit,activated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(provider_id) DO UPDATE SET
  provider_version=excluded.provider_version,provider_api_version=excluded.provider_api_version,
  bundle_sha256=excluded.bundle_sha256,manifest_sha256=excluded.manifest_sha256,
  module_sha256=excluded.module_sha256,source=excluded.source,
  release_repository=excluded.release_repository,release_tag=excluded.release_tag,
  release_commit=excluded.release_commit,activated_at_ms=excluded.activated_at_ms
`, provider.Active.ProviderID, provider.Active.ProviderVersion, provider.Active.ProviderAPI,
		provider.Active.BundleSHA256, provider.Active.ManifestSHA256, provider.Active.ModuleSHA256,
		provider.Source, repository, tag, commit, now)
	if err != nil {
		return fmt.Errorf("reconcile runtime providers: write provider: %w", err)
	}
	return nil
}

func writeTarget(
	ctx context.Context,
	transaction dbexec.Executor,
	providerID string,
	projected service.TargetProjection,
) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_targets(
	provider_id,target_id,display_name,target_options_schema_json,capabilities_json,checkpoint_json,manifest_fragment_json
) VALUES(?,?,?,?,?,?,?)
ON CONFLICT(provider_id,target_id) DO UPDATE SET
	display_name=excluded.display_name,
	target_options_schema_json=excluded.target_options_schema_json,
	capabilities_json=excluded.capabilities_json,checkpoint_json=excluded.checkpoint_json,
	manifest_fragment_json=excluded.manifest_fragment_json
	`, providerID, projected.Target.ID, projected.Target.DisplayName,
		projected.TargetOptionsJSON, projected.CapabilitiesJSON, projected.CheckpointJSON,
		projected.ManifestFragment)
	if err != nil {
		return fmt.Errorf("reconcile runtime providers: write target: %w", err)
	}
	return nil
}

func writeHostBindings(
	ctx context.Context,
	transaction dbexec.Executor,
	bindings []runtimecatalog.Binding,
) error {
	for _, binding := range bindings {
		if err := writeHostBinding(ctx, transaction, binding); err != nil {
			return err
		}
	}
	return nil
}

func writeHostBinding(ctx context.Context, transaction dbexec.Executor, binding runtimecatalog.Binding) error {
	strategy, registered := runtimecatalog.Strategy(binding.DetectorProfile)
	if !registered {
		return runtimecatalog.ErrCatalogInvalid
	}
	_, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_target_bindings(
 binding_id,core_id,provider_id,target_id,detector_profile,delivery_profile,launch_policy
) VALUES(?,?,?,?,?,?,?)
`, binding.ID, binding.CoreID, binding.ProviderID, binding.TargetID, binding.DetectorProfile,
		strategy.Delivery, binding.LaunchPolicy)
	if err != nil {
		return fmt.Errorf("reconcile runtime providers: write host binding: %w", err)
	}
	for _, platformID := range binding.PlatformIDs {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_binding_platforms(binding_id,platform_id,core_id) VALUES(?,?,?)
`, binding.ID, platformID, binding.CoreID); err != nil {
			return fmt.Errorf("reconcile runtime providers: write binding platform: %w", err)
		}
	}
	for _, contentKind := range binding.AcceptedContentKinds {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_binding_content_kinds(binding_id,content_kind) VALUES(?,?)
`, binding.ID, contentKind); err != nil {
			return fmt.Errorf("reconcile runtime providers: write binding content kind: %w", err)
		}
	}
	return nil
}

func writeCatalogState(
	ctx context.Context,
	transaction dbexec.Executor,
	candidate service.Projection,
	now int64,
) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_catalog_state(singleton,catalog_sha256,activated_at_ms)
VALUES(1,?,?)
ON CONFLICT(singleton) DO UPDATE SET
  catalog_sha256=excluded.catalog_sha256,activated_at_ms=excluded.activated_at_ms
`, candidate.CatalogSHA256, now)
	if err != nil {
		return fmt.Errorf("reconcile runtime providers: write catalog state: %w", err)
	}
	return nil
}

func synchronizeDefinitions(ctx context.Context, tx dbexec.Executor, candidate service.Projection, now int64) error {
	if err := runtimecatalogpersistence.SynchronizeDefinitions(ctx, tx, runtimecatalog.Catalog{
		SchemaVersion: 1, Definitions: candidate.Definitions, Bindings: candidate.Bindings,
	}, now); err != nil {
		return fmt.Errorf("project Host definitions: %w", err)
	}
	return nil
}
