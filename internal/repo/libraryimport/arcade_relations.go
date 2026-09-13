package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/libraryimport"
)

type ArcadeRelations struct{ executor dbexec.Executor }

func BindArcadeRelations(executor dbexec.Executor) *ArcadeRelations {
	return &ArcadeRelations{executor: executor}
}

func (records *ArcadeRelations) MachineRelation(
	ctx context.Context,
	datID, machine string,
) (application.ArcadeMachineRelation, bool, error) {
	var result application.ArcadeMachineRelation
	err := records.executor.QueryRowContext(ctx,
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
		return application.ArcadeMachineRelation{}, false, fmt.Errorf("read arcade machine: %w", err)
	}
	return result, true, nil
}
