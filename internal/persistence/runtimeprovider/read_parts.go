package runtimeprovider

import (
	"context"
	"fmt"

	"retrom/internal/dbexec"
	service "retrom/internal/service/runtimeprovider"

	"retrom/internal/cleanup"
)

func loadCurrentProviders(
	ctx context.Context,
	transaction dbexec.Executor,
) (map[string]service.CurrentProvider, error) {
	rows, err := transaction.QueryContext(ctx, `SELECT provider_id,provider_version,bundle_sha256 FROM runtime_providers`)
	if err != nil {
		return nil, fmt.Errorf("reconcile runtime providers: read providers: %w", err)
	}
	defer func() { cleanup.Error("close provider rows", rows.Close()) }()
	result := make(map[string]service.CurrentProvider)
	for rows.Next() {
		var providerID string
		var provider service.CurrentProvider
		if err := rows.Scan(&providerID, &provider.Version, &provider.BundleSHA256); err != nil {
			return nil, fmt.Errorf("reconcile runtime providers: scan provider: %w", err)
		}
		result[providerID] = provider
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reconcile runtime providers: providers: %w", err)
	}
	return result, nil
}

func (records catalogRecords) TargetReferenced(ctx context.Context, target service.TargetIdentity) (bool, error) {
	transaction := records.executor
	providerID, targetID := target.ProviderID, target.TargetID
	references := []struct{ table, providerColumn, targetColumn string }{
		{"bios_requirements", "provider_id", "target_id"},
		{"dat_versions", "provider_id", "target_id"},
		{"server_bios_import_items", "provider_id", "target_id"},
		{"import_jobs", "provider_id", "target_id"},
		{"import_item_core_validations", "provider_id", "target_id"},
		{"rpgmaker_review_profiles", "provider_id", "target_id"},
		{"review_preview_sessions", "provider_id", "target_id"},
		{"review_runtime_screenshots", "provider_id", "target_id"},
		{"game_variants", "provider_id", "target_id"},
		{"launch_sessions", "provider_id", "target_id"},
		{"netplay_sessions", "provider_id", "target_id"},
		{"pegasus_import_collections", "target_provider_id", "target_id"},
		{"emulationstation_import_collections", "target_provider_id", "target_id"},
	}
	for _, reference := range references {
		query := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE %s=? AND %s=? LIMIT 1)",
			reference.table, reference.providerColumn, reference.targetColumn)
		var exists bool
		if err := transaction.QueryRowContext(ctx, query, providerID, targetID).Scan(&exists); err != nil {
			return false, fmt.Errorf("reconcile runtime providers: inspect %s: %w", reference.table, err)
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}
