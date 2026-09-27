package gamecontent

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/gamecontent"
)

func (records impactRecords) counts(ctx context.Context, gameID string) (application.ImpactCounts, error) {
	var result application.ImpactCounts
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT
 (SELECT count(*) FROM save_states WHERE game_id=?),
 (SELECT count(*) FROM game_assets WHERE game_id=?),
 (SELECT count(*) FROM game_files file
  WHERE file.game_id=?),
 (SELECT count(*) FROM launch_sessions WHERE game_id=? AND state IN ('CREATED','ACTIVE'))
`, gameID, gameID, gameID, gameID).Scan(
		&result.SaveStates, &result.Assets, &result.ContentFiles,
		&result.ActiveLaunches,
	)
	if err != nil {
		return application.ImpactCounts{}, fmt.Errorf("gamecontent/delete impact counts: %w", err)
	}
	return result, nil
}
