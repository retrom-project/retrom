package gamecontent

import (
	"context"

	dbapi "retrom/internal/database"
	payloadpersistence "retrom/internal/persistence/payloadrelease"
)

func gameImpactBlobIDs(ctx context.Context, transaction dbapi.Executor, gameID string) ([]string, error) {
	ids, err := payloadpersistence.GameBlobIDs(ctx, transaction, gameID)
	if err != nil {
		return nil, err
	}
	importItems, err := payloadpersistence.CollectScopeIDs(ctx, transaction, `
SELECT metadata_source_ref_id FROM games WHERE id=? AND metadata_source_kind='IMPORT_REVIEW'
UNION SELECT content_source_ref_id FROM games WHERE id=? AND content_source_kind='IMPORT_REVIEW'
`, gameID, gameID)
	if err != nil {
		return nil, err
	}
	for _, itemID := range uniqueImpactStrings(importItems) {
		itemIDs, itemErr := payloadpersistence.ImportItemBlobIDs(ctx, transaction, itemID)
		if itemErr != nil {
			return nil, itemErr
		}
		ids = append(ids, itemIDs...)
	}
	sourceIDs, err := payloadpersistence.CollectScopeIDs(ctx, transaction, `
SELECT metadata_source_ref_id FROM games WHERE id=? AND metadata_source_kind='IMPORT_RECEIVE'
UNION SELECT content_source_ref_id FROM games WHERE id=? AND content_source_kind='IMPORT_RECEIVE'
`, gameID, gameID)
	if err != nil {
		return nil, err
	}
	for _, itemID := range uniqueImpactStrings(sourceIDs) {
		values, itemErr := payloadpersistence.CollectScopeIDs(ctx, transaction, `
SELECT blob_id FROM source_import_item_files WHERE item_id=?
UNION ALL SELECT source_archive_blob_id FROM source_import_item_files WHERE item_id=?
UNION ALL SELECT blob_id FROM source_import_item_assets WHERE item_id=?
`, itemID, itemID, itemID)
		if itemErr != nil {
			return nil, itemErr
		}
		ids = append(ids, values...)
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
