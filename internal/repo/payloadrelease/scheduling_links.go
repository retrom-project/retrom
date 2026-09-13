package payloadrelease

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/payloadrelease"
)

type sourceReleaseReader struct{ executor dbexec.Executor }

func BindReleases(executor dbexec.Executor) application.ReleaseScope {
	return application.ReleaseScope{Scheduling: BindScheduling(executor), Links: sourceReleaseReader{executor}}
}

func (reader sourceReleaseReader) RetainedSources(
	ctx context.Context, batch application.SourceBatch, after string, limit int,
) ([]string, error) {
	table := ""
	switch batch.Type {
	case application.ScopePegasusImportItem:
		table = "pegasus_import_items"
	case application.ScopeEmulationStationImportItem:
		table = "emulationstation_import_items"
	case application.ScopeImportItem, application.ScopeImportJob,
		application.ScopeUploadConsumption, application.ScopeGame, application.ScopeBlob:
		return nil, application.ErrScopeInvalid
	}
	return CollectScopeIDs(ctx, reader.executor, `SELECT id FROM `+table+`
WHERE import_id=? AND payload_state='RETAINED' AND id>? ORDER BY id LIMIT ?`, batch.ImportID, after, limit)
}

func (reader sourceReleaseReader) BoundSources(
	ctx context.Context, id string, after application.Scope, limit int,
) ([]application.Scope, error) {
	rows, err := reader.executor.QueryContext(ctx, `WITH sources AS (
SELECT 'PEGASUS_IMPORT_ITEM' AS kind,id FROM pegasus_import_items WHERE library_import_item_id=?
UNION ALL SELECT 'EMULATIONSTATION_IMPORT_ITEM',id FROM emulationstation_import_items WHERE library_import_item_id=?
) SELECT kind,id FROM sources WHERE kind>? OR (kind=? AND id>?) ORDER BY kind,id LIMIT ?`,
		id, id, after.Type, after.Type, after.ID, limit)
	if err != nil {
		return nil, fmt.Errorf("read bound source release links: %w", err)
	}
	defer func() { cleanup.Error("close source release links", rows.Close()) }()
	result := make([]application.Scope, 0)
	for rows.Next() {
		var ref application.Scope
		if err := rows.Scan(&ref.Type, &ref.ID); err != nil {
			return nil, fmt.Errorf("scan source release link: %w", err)
		}
		result = append(result, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source release links: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close source release links: %w", err)
	}
	return result, nil
}
