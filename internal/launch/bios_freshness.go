package launch

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
)

func (service *Service) currentBIOSMatchesDependencySnapshot(
	ctx context.Context,
	database corevalidation.Queryer,
	selection launchSelection,
) (bool, error) {
	if selection.compatibilityCode == reviewScreenshotOverrideCode {
		return true, nil
	}
	// Arcade locks archive files rather than the static BIOS snapshot shape.
	if selection.datID.Valid {
		return service.currentDATBIOSMatchesLockedFiles(ctx, database, selection)
	}
	current, _, _, err := corevalidation.ResolveBIOS(
		ctx, database, selection.providerID, selection.targetID, selection.contentLogicalName,
	)
	if err != nil {
		return false, fmt.Errorf("launch/resolve current BIOS: %w", err)
	}
	locked, err := corevalidation.ParseRuntimeBIOSDependencies(selection.dependencySnapshotJSON)
	if err != nil {
		// Provider-only project targets can legitimately carry an opaque empty
		// dependency snapshot. They are fresh when the Host has no BIOS facts.
		if len(current.BIOS) == 0 {
			return true, nil
		}
		return false, fmt.Errorf("launch/parse locked BIOS dependencies: %w", err)
	}
	current.BIOS = append([]corevalidation.BIOSDependency(nil), current.BIOS...)
	lockedSnapshot := corevalidation.Snapshot{
		SchemaVersion: corevalidation.SnapshotSchemaVersion, Kind: corevalidation.SnapshotKindStatic,
		BIOS: append([]corevalidation.BIOSDependency(nil), locked...),
	}
	currentDigest, err := corevalidation.BIOSDependencyDigest(current)
	if err != nil {
		return false, fmt.Errorf("launch/digest current BIOS dependencies: %w", err)
	}
	lockedDigest, err := corevalidation.BIOSDependencyDigest(lockedSnapshot)
	if err != nil {
		return false, fmt.Errorf("launch/digest locked BIOS dependencies: %w", err)
	}
	return currentDigest == lockedDigest, nil
}

func (service *Service) currentDATBIOSMatchesLockedFiles(
	ctx context.Context,
	database corevalidation.Queryer,
	selection launchSelection,
) (bool, error) {
	current, _, _, err := service.resolveVariantBIOS(
		ctx, database, selection.variantID, selection.gameID,
		selection.providerID, selection.targetID, selection.contentLogicalName, selection.datID,
	)
	if err != nil {
		return false, err
	}
	type lockedBIOS struct{ logicalName, blobID string }
	wanted := make([]lockedBIOS, 0, len(current.BIOS))
	for _, dependency := range current.BIOS {
		if dependency.DeliveryKind == "BIOS_BUNDLE" && dependency.BlobID != nil {
			wanted = append(wanted, lockedBIOS{dependency.LogicalName, *dependency.BlobID})
		}
	}
	rows, err := database.QueryContext(ctx, `
SELECT logical_name,blob_id FROM variant_files
WHERE game_variant_id=? AND role='BIOS_BUNDLE' ORDER BY sort_order,logical_name
`, selection.variantID)
	if err != nil {
		return false, fmt.Errorf("launch/query locked DAT BIOS files: %w", err)
	}
	defer func() { cleanup.Error("close current DAT BIOS files", rows.Close()) }()
	locked := make([]lockedBIOS, 0, len(wanted))
	for rows.Next() {
		var file lockedBIOS
		if err := rows.Scan(&file.logicalName, &file.blobID); err != nil {
			return false, fmt.Errorf("launch/scan locked DAT BIOS file: %w", err)
		}
		locked = append(locked, file)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("launch/iterate locked DAT BIOS files: %w", err)
	}
	return slices.Equal(wanted, locked), nil
}

// Recheck inside the transaction that freezes the files. A concurrent BIOS switch
// or validation must never pair an old snapshot with newly selected file bytes.
func (service *Service) checkLaunchBIOSSelection(
	ctx context.Context, database *sql.Tx, selection launchSelection,
) error {
	var snapshot string
	if err := database.QueryRowContext(ctx, `
SELECT dependency_snapshot_json FROM game_variants WHERE id=? AND status='READY'
`, selection.variantID).Scan(&snapshot); err != nil {
		return ErrBlocked
	}
	if snapshot != selection.dependencySnapshotJSON {
		return ErrBlocked
	}
	fresh, err := service.currentBIOSMatchesDependencySnapshot(ctx, database, selection)
	if err != nil || !fresh {
		return ErrBlocked
	}
	return nil
}

// Validation can run outside the write transaction. Refuse results whose BIOS
// was replaced while the worker was preparing its evidence.
func (service *Service) checkValidationBIOS(
	ctx context.Context, tx *sql.Tx, inputs validationInputs, datID sql.NullString,
) error {
	var logicalName string
	err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT logical_name FROM game_files
WHERE game_id=? AND role IN ('CONTENT','DISC')
ORDER BY CASE role WHEN 'CONTENT' THEN 0 ELSE 1 END,sort_order,logical_name LIMIT 1),'')
`, inputs.GameID).Scan(&logicalName)
	if err != nil {
		return fmt.Errorf("validation BIOS content: %w", err)
	}
	current, _, _, err := service.resolveVariantBIOS(
		ctx, tx, inputs.GameVariantID, inputs.GameID, inputs.ProviderID, inputs.TargetID, logicalName, datID,
	)
	if err != nil {
		return err
	}
	digest, err := corevalidation.BIOSDependencyDigest(current)
	if err != nil {
		return fmt.Errorf("validation BIOS digest: %w", err)
	}
	if digest != inputs.BIOSDependencyDigest {
		return errValidationGameChanged
	}
	return nil
}

func (service *Service) prepareCurrentBIOS(
	ctx context.Context, tx *sql.Tx, selection launchSelection,
) (launchSelection, error) {
	if err := service.checkLaunchBIOSSelection(ctx, tx, selection); err != nil {
		return selection, err
	}
	return service.refreshOverrideBIOS(ctx, tx, selection)
}
