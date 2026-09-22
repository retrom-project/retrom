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
