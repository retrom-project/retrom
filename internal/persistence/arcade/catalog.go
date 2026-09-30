package arcade

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/content/arcade"
	dbapi "retrom/internal/database"
)

type Catalog struct{ executor dbapi.Executor }

func New(executor dbapi.Executor) *Catalog { return &Catalog{executor: executor} }
func (records *Catalog) ActiveDAT(ctx context.Context, providerID, targetID string) (string, error) {
	var id string
	err := dbapi.QueryRowContext(ctx, records.executor,
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

func (records *Catalog) MachineClassification(
	ctx context.Context, datID, machine string,
) (string, bool, error) {
	var classification string
	err := dbapi.QueryRowContext(ctx, records.executor,
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

func (records *Catalog) MachineRelation(
	ctx context.Context,
	datID, machine string,
) (arcade.MachineRelation, bool, error) {
	var result arcade.MachineRelation
	err := dbapi.QueryRowContext(ctx, records.executor,
		`SELECT COALESCE(cloneof,
''),
COALESCE(romof,
'') FROM dat_machines WHERE dat_version_id=? AND machine_name=?`,
		datID,
		machine).Scan(&result.CloneOf,
		&result.ROMOf)
	if errors.Is(err, sql.ErrNoRows) {
		return result, false, nil
	}
	if err != nil {
		return arcade.MachineRelation{}, false, fmt.Errorf("read arcade machine: %w", err)
	}
	return result, true, nil
}
