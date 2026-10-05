package runtimeprovider

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	runtimebundle "retrom/internal/runtime/bundle"
)

func writeTargetInputLimits(ctx context.Context, transaction dbapi.Tx, providerID string,
	target runtimebundle.Target, contents []byte,
) error {
	if _, err := transaction.ExecContext(ctx,
		`DELETE FROM runtime_target_input_limits WHERE provider_id=? AND target_id=?`, providerID, target.ID,
	); err != nil {
		return fmt.Errorf("replace target input limits: %w", err)
	}
	for _, input := range target.Inputs {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO runtime_target_input_limits
(provider_id,target_id,role,max_file_bytes) VALUES(?,?,?,?)`, providerID, target.ID, input.Role, input.MaxFileBytes,
		); err != nil {
			return fmt.Errorf("write target input limit: %w", err)
		}
	}
	if len(contents) != 0 {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO runtime_requirement_catalogs(sha256,document_json) VALUES(?,?)
ON CONFLICT(sha256) DO UPDATE SET document_json=excluded.document_json`,
			target.ContentRequirements.Catalog.SHA256, string(contents),
		); err != nil {
			return fmt.Errorf("write target content catalog: %w", err)
		}
	}
	return nil
}
