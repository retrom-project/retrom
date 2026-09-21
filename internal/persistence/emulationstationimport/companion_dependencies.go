package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
)

func (records companionRecords) Dependencies(ctx context.Context, datVersionID, machine string) ([]string, error) {
	rows, err := records.executor.QueryContext(
		ctx,
		`WITH RECURSIVE dependency(machine) AS (
 SELECT cloneof FROM dat_machines
 WHERE dat_version_id=? AND machine_name=? AND cloneof IS NOT NULL
 UNION
 SELECT romof FROM dat_machines
 WHERE dat_version_id=? AND machine_name=? AND romof IS NOT NULL
 UNION
 SELECT relation.cloneof FROM dat_machines relation
 JOIN dependency current ON relation.machine_name=current.machine
 WHERE relation.dat_version_id=? AND relation.cloneof IS NOT NULL
 UNION
 SELECT relation.romof FROM dat_machines relation
 JOIN dependency current ON relation.machine_name=current.machine
 WHERE relation.dat_version_id=? AND relation.romof IS NOT NULL
)
SELECT machine FROM dependency WHERE machine<>? ORDER BY machine`,
		datVersionID,
		machine,
		datVersionID,
		machine,
		datVersionID,
		datVersionID,
		machine,
	)
	if err != nil {
		return nil, fmt.Errorf("query EmulationStation companion closure: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := []string{}
	for rows.Next() {
		var dependency string
		if err := rows.Scan(&dependency); err != nil {
			return nil, fmt.Errorf("read EmulationStation companion dependency: %w", err)
		}
		result = append(result, dependency)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate EmulationStation companion closure: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close EmulationStation companion closure: %w", err)
	}
	return result, nil
}
