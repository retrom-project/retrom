package launch

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/corevalidation"
)

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
