package storageanalysis

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/storageanalysis"
	"retrom/internal/repo/blobregistry"
)

var errReferenceCoverage = errors.New("STORAGE_ANALYSIS_REFERENCE_COVERAGE_MISMATCH")

var referenceUsage = map[string]storageanalysis.Usage{
	"bios_installations.blob_id":                                storageanalysis.UsageBIOS,
	"content_hash_evidence.archive_blob_id":                     storageanalysis.UsageWorkflow,
	"content_hash_evidence.blob_id":                             storageanalysis.UsageWorkflow,
	"emulationstation_import_item_assets.blob_id":               storageanalysis.UsageWorkflow,
	"emulationstation_import_item_files.blob_id":                storageanalysis.UsageWorkflow,
	"emulationstation_import_item_files.source_archive_blob_id": storageanalysis.UsageWorkflow,
	"game_assets.blob_id":                                       storageanalysis.UsageMedia,
	"game_files.blob_id":                                        storageanalysis.UsageGame,
	"game_files.source_archive_blob_id":                         storageanalysis.UsageGame,
	"import_item_source_files.blob_id":                          storageanalysis.UsageWorkflow,
	"import_item_source_files.source_archive_blob_id":           storageanalysis.UsageWorkflow,
	"import_item_source_snapshot_files.blob_id":                 storageanalysis.UsageWorkflow,
	"import_item_source_snapshot_files.source_archive_blob_id":  storageanalysis.UsageWorkflow,
	"import_item_multidisc_entries.blob_id":                     storageanalysis.UsageWorkflow,
	"import_item_validation_files.blob_id":                      storageanalysis.UsageWorkflow,
	"review_arcade_parent_attachments.accepted_blob_id":         storageanalysis.UsageWorkflow,
	"launch_game_save_bindings.restore_payload_blob_id":         storageanalysis.UsageRuntime,
	"launch_content_files.blob_id":                              storageanalysis.UsageRuntime,
	"launch_external_files.blob_id":                             storageanalysis.UsageRuntime,
	"metadata_provider_responses.raw_response_blob_id":          storageanalysis.UsageWorkflow,
	"pegasus_import_item_assets.blob_id":                        storageanalysis.UsageWorkflow,
	"pegasus_import_item_files.blob_id":                         storageanalysis.UsageWorkflow,
	"pegasus_import_item_files.source_archive_blob_id":          storageanalysis.UsageWorkflow,
	"review_uploaded_assets.blob_id":                            storageanalysis.UsageWorkflow,
	"review_preview_sessions.content_blob_id":                   storageanalysis.UsageWorkflow,
	"review_preview_sessions.checkpoint_payload_blob_id":        storageanalysis.UsageWorkflow,
	"review_preview_sessions.restore_payload_blob_id":           storageanalysis.UsageWorkflow,
	"review_preview_files.blob_id":                              storageanalysis.UsageWorkflow,
	"review_runtime_screenshots.blob_id":                        storageanalysis.UsageWorkflow,
	"runtime_asset_pack_files.blob_id":                          storageanalysis.UsageBIOS,
	"runtime_asset_pack_installations.bundle_blob_id":           storageanalysis.UsageBIOS,
	"save_states.payload_blob_id":                               storageanalysis.UsageSaves,
	"save_states.screenshot_blob_id":                            storageanalysis.UsageSaves,
	"scrape_candidate_assets.blob_id":                           storageanalysis.UsageWorkflow,
	"upload_files.final_blob_id":                                storageanalysis.UsageWorkflow,
	"variant_files.blob_id":                                     0,
}

func validateReferenceCoverage(edges []blobregistry.Edge) error {
	seen := make(map[string]struct{}, len(referenceUsage))
	for _, edge := range edges {
		if edge.Class != "PROTECTIVE" {
			continue
		}
		key := edge.Table + "." + edge.Column
		if _, ok := referenceUsage[key]; !ok {
			return fmt.Errorf("%w: missing %s", errReferenceCoverage, key)
		}
		seen[key] = struct{}{}
	}
	for key := range referenceUsage {
		if _, ok := seen[key]; !ok {
			return fmt.Errorf("%w: stale %s", errReferenceCoverage, key)
		}
	}
	return nil
}

func loadUsage(
	ctx context.Context,
	transaction *sql.Tx,
	edges []blobregistry.Edge,
) (map[string]storageanalysis.Usage, error) {
	result := map[string]storageanalysis.Usage{}
	for _, edge := range edges {
		if edge.Class != "PROTECTIVE" {
			continue
		}
		key := edge.Table + "." + edge.Column
		if key == "variant_files.blob_id" {
			if err := loadVariantUsage(ctx, transaction, result); err != nil {
				return nil, err
			}
			continue
		}
		query := `SELECT DISTINCT ` + quote(edge.Column) + ` FROM ` + quote(edge.Table) +
			` WHERE ` + quote(edge.Column) + ` IS NOT NULL`
		if err := collectUsage(ctx, transaction, query, referenceUsage[key], result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func loadVariantUsage(ctx context.Context, transaction *sql.Tx, result map[string]storageanalysis.Usage) error {
	rows, err := transaction.QueryContext(ctx, `SELECT DISTINCT blob_id, role FROM variant_files`)
	if err != nil {
		return fmt.Errorf("storageanalysis/references: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var id, role string
		if err := rows.Scan(&id, &role); err != nil {
			return fmt.Errorf("storageanalysis/references: %w", err)
		}
		flag := storageanalysis.UsageGame
		if role == "BIOS_BUNDLE" {
			flag = storageanalysis.UsageBIOS
		}
		result[id] |= flag
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storageanalysis/references: %w", err)
	}
	return nil
}

func collectUsage(
	ctx context.Context,
	transaction *sql.Tx,
	query string,
	flag storageanalysis.Usage,
	result map[string]storageanalysis.Usage,
) error {
	rows, err := transaction.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("storageanalysis/references: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("storageanalysis/references: %w", err)
		}
		result[id] |= flag
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storageanalysis/references: %w", err)
	}
	return nil
}

func quote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
