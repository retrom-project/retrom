package launch

import (
	"context"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/launch"
)

type productValidationRecords struct {
	*ValidationJobs
	executor dbexec.Executor
}

func (records productValidationRecords) CreateVariant(ctx context.Context, plan application.ProductVariantWrite) error {
	source := plan.Source
	if _, err := recordstore.CreateGameVariants(
		ctx,
		records.executor,
		`INSERT INTO game_variants(
 id,game_id,core_id,provider_id,target_id,dat_version_id,emulator_game_id,
 status,compatibility_code,dependency_snapshot_json,default_dos_entry,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,NULL,'BLOCKED','VALIDATION_PENDING','{}',NULL,1,?,?)`,
		source.VariantID,
		source.GameID,
		source.CoreID,
		source.ProviderID,
		source.TargetID,
		source.ActiveDATVersionID,
		plan.NowMS,
		plan.NowMS,
	); err != nil {
		return fmt.Errorf("create product variant: %w", err)
	}
	return nil
}

func (records productValidationRecords) MarkPending(ctx context.Context, variantID string, now int64) error {
	result, err := recordstore.UpdateGameVariants(ctx, records.executor, recordstore.Update{
		Set: `status='BLOCKED',compatibility_code='VALIDATION_PENDING',
emulator_game_id=NULL,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=?`, Args: []any{variantID}}, Values: []any{now},
	})
	if err != nil {
		return fmt.Errorf("mark product validation pending: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count pending product variant: %w", err)
	}
	if count != 1 {
		return application.ErrBlocked
	}
	return nil
}
