package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

type ArcadeRelations struct{ executor dbapi.Executor }

func BindArcadeRelations(executor dbapi.Executor) *ArcadeRelations {
	return &ArcadeRelations{executor: executor}
}

func (records *ArcadeRelations) MachineRelation(
	ctx context.Context,
	datID, machine string,
) (libraryservice.ArcadeMachineRelation, bool, error) {
	var result libraryservice.ArcadeMachineRelation
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
		return libraryservice.ArcadeMachineRelation{}, false, fmt.Errorf("read arcade machine: %w", err)
	}
	return result, true, nil
}
