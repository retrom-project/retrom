package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/dbexec"
	application "retrom/internal/service/libraryimport"
)

type PreparationCatalog struct{ executor dbexec.Executor }

func BindPreparationCatalog(executor dbexec.Executor) *PreparationCatalog {
	return &PreparationCatalog{executor: executor}
}

func (records *PreparationCatalog) ActiveDAT(ctx context.Context, providerID, targetID string) (string, error) {
	var id string
	err := records.executor.QueryRowContext(ctx,
		`SELECT id FROM dat_versions WHERE provider_id=? AND target_id=? AND is_active=1`,
		providerID, targetID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read active preparation DAT: %w", err)
	}
	return id, nil
}

func (records *PreparationCatalog) MachineClassification(
	ctx context.Context, datID, machine string,
) (string, bool, error) {
	var classification string
	err := records.executor.QueryRowContext(ctx,
		`SELECT classification FROM dat_machines WHERE dat_version_id=? AND machine_name=?`,
		datID, machine).Scan(&classification)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read preparation arcade classification: %w", err)
	}
	return classification, true, nil
}

func (records *PreparationCatalog) ArcadeRequirements(
	ctx context.Context, datID, machine string,
) (application.ArcadeCatalogRequirements, error) {
	return (approvalDependencyRecords{executor: records.executor}).ArcadeRequirements(ctx, datID, machine)
}

func (records *PreparationCatalog) MachineRelation(
	ctx context.Context, datID, machine string,
) (application.ArcadeMachineRelation, bool, error) {
	return BindArcadeRelations(records.executor).MachineRelation(ctx, datID, machine)
}
