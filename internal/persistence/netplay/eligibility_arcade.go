package netplay

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/service/netplay"
)

func (repository *Eligibility) ArcadeDependencies(
	ctx context.Context, variantID, datID string,
) ([]netplay.ArcadeDependencyRow, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT kind,logical_archive,source_machine_name,required_entries_json,state
FROM variant_dependencies WHERE game_variant_id=? AND dat_version_id=?
ORDER BY kind,logical_archive
`, variantID, datID)
	if err != nil {
		return nil, fmt.Errorf("netplay/load Arcade dependencies: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]netplay.ArcadeDependencyRow, 0)
	for rows.Next() {
		var row netplay.ArcadeDependencyRow
		if err := rows.Scan(&row.Kind, &row.LogicalArchive, &row.Machine, &row.RequiredEntriesJSON, &row.State); err != nil {
			return nil, fmt.Errorf("netplay/scan Arcade dependency: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netplay/iterate Arcade dependencies: %w", err)
	}
	return result, nil
}

func (repository *Eligibility) DependencyFileCount(
	ctx context.Context, variantID, role, logicalName string,
) (int, error) {
	var count int
	err := repository.database.QueryRowContext(ctx, `
SELECT count(*) FROM variant_files WHERE game_variant_id=? AND role=? AND logical_name=?
`, variantID, role, logicalName).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("netplay/check Arcade dependency file: %w", err)
	}
	return count, nil
}
