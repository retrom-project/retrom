package launch

import (
	"context"
	"fmt"

	"retrom/internal/corevalidation"
	"retrom/internal/scummvm"
)

func (service *Service) scummVMValidationOutcome(
	ctx context.Context,
	inputs validationInputs,
) (variantValidationOutcome, error) {
	var raw string
	if err := service.database.QueryRowContext(ctx, `
SELECT dependency_snapshot_json
FROM game_variants
WHERE id=? AND game_id=? AND provider_id='retrom-runtime' AND target_id='scummvm'
`, inputs.GameVariantID, inputs.GameID).Scan(&raw); err != nil {
		return variantValidationOutcome{}, fmt.Errorf("read ScummVM selection for validation: %w", err)
	}
	snapshot, err := scummvm.ParseSnapshot(raw)
	if err != nil || snapshot.Detection.SourceDigest != inputs.SourceManifestDigest {
		return variantValidationOutcome{}, errValidationGameChanged
	}
	status, code := snapshot.Status()
	return variantValidationOutcome{
		status: status, code: code, dependencySnapshotJSON: raw,
		biosSnapshot: corevalidation.Snapshot{
			SchemaVersion: 1, Kind: corevalidation.SnapshotKindStatic, BIOS: []corevalidation.BIOSDependency{},
		},
	}, nil
}
