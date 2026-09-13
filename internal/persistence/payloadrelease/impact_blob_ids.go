package payloadrelease

import (
	"context"

	"retrom/internal/persistence/dbexec"
)

func gameImpactBlobIDs(ctx context.Context, transaction dbexec.Executor, gameID string) ([]string, error) {
	ids, err := GameBlobIDs(ctx, transaction, gameID)
	if err != nil {
		return nil, err
	}
	importItems, err := collectIDs(ctx, transaction, `
SELECT metadata_source_ref_id FROM games WHERE id=? AND metadata_source_kind='IMPORT_REVIEW'
UNION SELECT content_source_ref_id FROM games WHERE id=? AND content_source_kind='IMPORT_REVIEW'
`, gameID, gameID)
	if err != nil {
		return nil, err
	}
	for _, itemID := range uniqueStrings(importItems) {
		itemIDs, itemErr := ImportItemBlobIDs(ctx, transaction, itemID)
		if itemErr != nil {
			return nil, itemErr
		}
		ids = append(ids, itemIDs...)
	}
	pegasusIDs, err := collectIDs(ctx, transaction, `
SELECT metadata_source_ref_id FROM games WHERE id=? AND metadata_source_kind='SERVER_PEGASUS_IMPORT'
UNION SELECT content_source_ref_id FROM games WHERE id=? AND content_source_kind='SERVER_PEGASUS_IMPORT'
`, gameID, gameID)
	if err != nil {
		return nil, err
	}
	for _, itemID := range uniqueStrings(pegasusIDs) {
		values, itemErr := collectIDs(ctx, transaction, `
SELECT blob_id FROM pegasus_import_item_files WHERE item_id=?
UNION ALL SELECT source_archive_blob_id FROM pegasus_import_item_files WHERE item_id=?
UNION ALL SELECT blob_id FROM pegasus_import_item_assets WHERE item_id=?
`, itemID, itemID, itemID)
		if itemErr != nil {
			return nil, itemErr
		}
		ids = append(ids, values...)
	}
	emulationStationIDs, err := collectIDs(ctx, transaction, `
SELECT metadata_source_ref_id FROM games
 WHERE id=? AND metadata_source_kind='SERVER_EMULATIONSTATION_IMPORT'
UNION SELECT content_source_ref_id FROM games
 WHERE id=? AND content_source_kind='SERVER_EMULATIONSTATION_IMPORT'
`, gameID, gameID)
	if err != nil {
		return nil, err
	}
	for _, itemID := range uniqueStrings(emulationStationIDs) {
		values, itemErr := collectIDs(ctx, transaction, `
SELECT blob_id FROM emulationstation_import_item_files WHERE item_id=?
UNION ALL SELECT source_archive_blob_id FROM emulationstation_import_item_files WHERE item_id=?
UNION ALL SELECT blob_id FROM emulationstation_import_item_assets WHERE item_id=?
`, itemID, itemID, itemID)
		if itemErr != nil {
			return nil, itemErr
		}
		ids = append(ids, values...)
	}
	return uniqueStrings(ids), nil
}
