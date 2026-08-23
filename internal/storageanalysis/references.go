package storageanalysis

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/blobregistry"
	"retrom/internal/cleanup"
)

var errReferenceCoverage = errors.New("STORAGE_ANALYSIS_REFERENCE_COVERAGE_MISMATCH")

var referenceUsage = map[string]usage{
	"bios_installations.blob_id":                                usageBIOS,
	"content_hash_evidence.archive_blob_id":                     usageWorkflow,
	"content_hash_evidence.blob_id":                             usageWorkflow,
	"emulationstation_import_item_assets.blob_id":               usageWorkflow,
	"emulationstation_import_item_files.blob_id":                usageWorkflow,
	"emulationstation_import_item_files.source_archive_blob_id": usageWorkflow,
	"game_assets.blob_id":                                       usageMedia,
	"game_content_files.blob_id":                                usageGame,
	"game_content_files.source_archive_blob_id":                 usageGame,
	"import_item_source_files.blob_id":                          usageWorkflow,
	"import_item_source_files.source_archive_blob_id":           usageWorkflow,
	"import_item_source_snapshot_files.blob_id":                 usageWorkflow,
	"import_item_source_snapshot_files.source_archive_blob_id":  usageWorkflow,
	"import_item_multidisc_entries.blob_id":                     usageWorkflow,
	"import_item_validation_files.blob_id":                      usageWorkflow,
	"review_arcade_parent_attachments.accepted_blob_id":         usageWorkflow,
	"launch_content_files.blob_id":                              usageRuntime,
	"launch_external_files.blob_id":                             usageRuntime,
	"metadata_provider_responses.raw_response_blob_id":          usageWorkflow,
	"pegasus_import_item_assets.blob_id":                        usageWorkflow,
	"pegasus_import_item_files.blob_id":                         usageWorkflow,
	"pegasus_import_item_files.source_archive_blob_id":          usageWorkflow,
	"review_uploaded_assets.blob_id":                            usageWorkflow,
	"review_preview_sessions.content_blob_id":                   usageWorkflow,
	"review_preview_files.blob_id":                              usageWorkflow,
	"review_runtime_screenshots.blob_id":                        usageWorkflow,
	"save_states.screenshot_blob_id":                            usageSaves,
	"save_states.state_blob_id":                                 usageSaves,
	"scrape_candidate_assets.blob_id":                           usageWorkflow,
	"upload_files.final_blob_id":                                usageWorkflow,
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

func loadUsage(ctx context.Context, transaction *sql.Tx, edges []blobregistry.Edge) (map[string]usage, error) {
	result := map[string]usage{}
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

func loadVariantUsage(ctx context.Context, transaction *sql.Tx, result map[string]usage) error {
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
		flag := usageGame
		if role == "BIOS_BUNDLE" {
			flag = usageBIOS
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
	flag usage,
	result map[string]usage,
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

func propagateArchiveUsage(
	ctx context.Context,
	transaction *sql.Tx,
	protected map[string]struct{},
	result map[string]usage,
) error {
	rows, err := transaction.QueryContext(ctx, `
SELECT archive_blob_id, materialized_blob_id
FROM archive_entries
WHERE materialized_blob_id IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("storageanalysis/references: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var owner, member string
		if err := rows.Scan(&owner, &member); err != nil {
			return fmt.Errorf("storageanalysis/references: %w", err)
		}
		if _, ok := protected[owner]; ok {
			result[member] |= result[owner]
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storageanalysis/references: %w", err)
	}
	return nil
}

func quote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
