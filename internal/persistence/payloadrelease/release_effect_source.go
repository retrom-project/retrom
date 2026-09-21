package payloadrelease

import (
	"context"
	"database/sql"

	"retrom/internal/dbexec"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/payloadrelease"
)

type (
	effectRecordUpdate func(context.Context, dbexec.Executor, recordstore.Update) (sql.Result, error)
	effectSourceTables struct {
		itemsTable, filesTable, assetsTable   string
		updateItem, updateFiles, updateAssets effectRecordUpdate
	}
)

func effectSourceSpec(kind application.ScopeType) (effectSourceTables, error) {
	switch kind {
	case application.ScopePegasusImportItem:
		return effectSourceTables{
			itemsTable:   "pegasus_import_items",
			filesTable:   "pegasus_import_item_files",
			assetsTable:  "pegasus_import_item_assets",
			updateItem:   recordstore.UpdatePegasusImportItems,
			updateFiles:  recordstore.UpdatePegasusImportItemFiles,
			updateAssets: recordstore.UpdatePegasusImportItemAssets,
		}, nil
	case application.ScopeEmulationStationImportItem:
		return effectSourceTables{
			itemsTable:   "emulationstation_import_items",
			filesTable:   "emulationstation_import_item_files",
			assetsTable:  "emulationstation_import_item_assets",
			updateItem:   recordstore.UpdateEmulationstationImportItems,
			updateFiles:  recordstore.UpdateEmulationstationImportItemFiles,
			updateAssets: recordstore.UpdateEmulationstationImportItemAssets,
		}, nil
	case application.ScopeImportItem, application.ScopeImportJob, application.ScopeUploadConsumption,
		application.ScopeGame, application.ScopeBlob:
		return effectSourceTables{}, application.ErrScopeInvalid
	default:
		return effectSourceTables{}, application.ErrScopeInvalid
	}
}
