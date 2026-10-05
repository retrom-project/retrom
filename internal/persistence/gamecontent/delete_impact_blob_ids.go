package gamecontent

import (
	"context"

	dbapi "retrom/internal/database"
)

func gameImpactFileRecords(ctx context.Context, transaction dbapi.Executor, gameID string) ([]string, error) {
	ids, err := dbapi.QueryStrings(
		ctx,
		transaction,
		`SELECT file_record FROM game_files WHERE game_id=$1
UNION SELECT file_record FROM game_assets WHERE game_id=$1
UNION SELECT f.file_record FROM variant_files f JOIN game_variants v ON v.id=f.game_variant_id
 WHERE v.game_id=$1 AND f.role<>'BIOS_BUNDLE'
UNION SELECT payload_file_record FROM save_states WHERE game_id=$1 AND payload_file_record IS NOT NULL
UNION SELECT screenshot_file_record FROM save_states WHERE game_id=$1 AND screenshot_file_record IS NOT NULL`,
		gameID,
	)
	if err != nil {
		return nil, wrapErr(err)
	}
	return uniqueImpactStrings(ids), nil
}

func uniqueImpactStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
