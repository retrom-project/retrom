package payloadrelease

import (
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
	application "retrom/internal/service/payloadrelease"
)

type (
	effectSourceTables struct {
		itemsTable, filesTable, assetsTable   string
		updateItem, updateFiles, updateAssets releaseops.RecordUpdate
	}
)

func effectSourceSpec(kind application.ScopeType) (effectSourceTables, error) {
	switch kind {
	case application.ScopeSourceImportItem:
		return effectSourceTables{
			itemsTable:   "source_import_items",
			filesTable:   "source_import_item_files",
			assetsTable:  "source_import_item_assets",
			updateItem:   recordstore.UpdateSourceImportItems,
			updateFiles:  recordstore.UpdateSourceImportItemFiles,
			updateAssets: recordstore.UpdateSourceImportItemAssets,
		}, nil

	case application.ScopeImportItem, application.ScopeImportJob, application.ScopeUploadConsumption,
		application.ScopeGame, application.ScopeBlob:
		return effectSourceTables{}, application.ErrScopeInvalid
	default:
		return effectSourceTables{}, application.ErrScopeInvalid
	}
}
