package gamevariant

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/gamevariant"
)

type writeRecords struct {
	*ValidationJobs
	executor dbapi.Executor
}

func (records writeRecords) CreateVariant(ctx context.Context, plan application.VariantWrite) error {
	source := plan.Source
	dependencySnapshot := "{}"
	if plan.Arcade != nil {
		dependencySnapshot = plan.Arcade.Snapshot
	}
	if _, err := recordstore.CreateGameVariants(
		ctx,
		records.executor,
		`INSERT INTO game_variants(
 id,game_id,core_id,provider_id,target_id,dat_version_id,emulator_game_id,
 status,compatibility_code,dependency_snapshot_json,default_dos_entry,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,NULL,'BLOCKED','VALIDATION_PENDING',?,NULL,1,?,?)`,
		source.VariantID,
		source.GameID,
		source.CoreID,
		source.ProviderID,
		source.TargetID,
		source.ActiveDATVersionID,
		dependencySnapshot,
		plan.NowMS,
		plan.NowMS,
	); err != nil {
		return fmt.Errorf("create product variant: %w", err)
	}
	if plan.Arcade != nil {
		for _, file := range plan.Arcade.Files {
			if _, err := recordstore.CreateVariantFiles(ctx, records.executor, `
INSERT INTO variant_files(game_variant_id,role,logical_name,file_record,sort_order)
VALUES(?,?,?,?,?)`, source.VariantID, file.Role, file.LogicalName, file.FileRecord, file.SortOrder); err != nil {
				return fmt.Errorf("create alternate arcade file: %w", err)
			}
		}
		for _, dependency := range plan.Arcade.Dependencies {
			if _, err := records.executor.ExecContext(ctx, `
INSERT INTO variant_dependencies(game_variant_id,kind,logical_archive,dat_version_id,
source_machine_name,required_entries_json,state,created_at_ms)
VALUES(?,?,?,?,?,?,?,?)`, source.VariantID, dependency.Kind, dependency.Machine+".zip",
				source.ActiveDATVersionID, dependency.Machine, dependency.RequiredEntriesJSON,
				dependency.State, plan.NowMS); err != nil {
				return fmt.Errorf("create alternate arcade dependency: %w", err)
			}
		}
	}
	return nil
}

func (records writeRecords) MarkPending(ctx context.Context, variantID string, now int64) error {
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

func NewWriteScope(executor dbapi.Executor) application.WriteScope {
	return writeRecords{ValidationJobs: NewValidationJobs(executor), executor: executor}
}
