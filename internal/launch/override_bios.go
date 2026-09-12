package launch

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/recordstore"

	"retrom/internal/corevalidation"
)

// Manual approval is retained. Only currently installed managed BIOS inputs are
// refreshed; unrelated manually supplied Arcade files are not discarded.
func (service *Service) refreshOverrideBIOS(
	ctx context.Context, tx *sql.Tx, selection launchSelection,
) (launchSelection, error) {
	if selection.compatibilityCode != reviewScreenshotOverrideCode {
		return selection, nil
	}
	current, _, _, err := service.resolveVariantBIOS(
		ctx, tx, selection.variantID, selection.gameID, selection.providerID, selection.targetID,
		selection.contentLogicalName, selection.datID,
	)
	if err != nil {
		return selection, err
	}
	if len(current.BIOS) == 0 {
		return selection, nil
	}
	encoded, err := approvedBIOSSnapshot(selection, current)
	if err != nil {
		return selection, err
	}
	if selection.dependencySnapshotJSON != encoded {
		selection.dependencySnapshotJSON = encoded
		_, err = recordstore.UpdateGameVariants(ctx, tx, recordstore.Update{
			Set: `dependency_snapshot_json=?,version=version+1,updated_at_ms=?`,
			Scope: recordstore.Scope{
				Where: `id=?`,
				Args:  []any{selection.variantID},
			},
			Values: []any{encoded, service.now().UnixMilli()},
		})
		if err != nil {
			return selection, fmt.Errorf("refresh approved BIOS snapshot: %w", err)
		}
	}
	for sortOrder, dependency := range current.BIOS {
		if dependency.DeliveryKind != "BIOS_BUNDLE" || dependency.BlobID == nil {
			continue
		}
		_, err := recordstore.CreateVariantFiles(ctx, tx, `
INSERT INTO variant_files(game_variant_id,role,logical_name,blob_id,sort_order)
VALUES(?,'BIOS_BUNDLE',?,?,?) ON CONFLICT(game_variant_id,role,logical_name)
DO UPDATE SET blob_id=excluded.blob_id,sort_order=excluded.sort_order
WHERE variant_files.blob_id<>excluded.blob_id
`, selection.variantID, dependency.LogicalName, *dependency.BlobID, sortOrder)
		if err != nil {
			return selection, fmt.Errorf("refresh approved BIOS files: %w", err)
		}
	}
	return selection, nil
}

func approvedBIOSSnapshot(selection launchSelection, current corevalidation.Snapshot) (string, error) {
	if selection.datID.Valid {
		return selection.dependencySnapshotJSON, nil
	}
	snapshot, err := corevalidation.ParseSnapshot(selection.dependencySnapshotJSON)
	if err != nil {
		return "", fmt.Errorf("parse approved BIOS snapshot: %w", err)
	}
	snapshot.BIOS = current.BIOS
	encoded, err := snapshot.JSON()
	if err != nil {
		return "", fmt.Errorf("encode approved BIOS snapshot: %w", err)
	}
	return string(encoded), nil
}
