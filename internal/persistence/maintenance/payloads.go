package maintenance

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	release "retrom/internal/persistence/payloadrelease"
	application "retrom/internal/service/maintenance"
	"retrom/internal/service/payloadrelease"
)

type payloadRecords struct{ executor dbexec.Executor }

func (writes writes) Imports() application.RestoredImportScope {
	return application.RestoredImportScope{
		Reviews: writes.Reviews(),
		Payloads: application.RestoredPayloadScope{
			Records: payloadRecords{writes.transaction}, Scheduling: release.BindScheduling(writes.transaction),
		},
	}
}

func (records payloadRecords) RetainedSources(
	ctx context.Context, query application.RestoredPayloadQuery,
) ([]string, error) {
	var table string
	switch query.Kind {
	case payloadrelease.ScopePegasusImportItem:
		table = "pegasus_import_items"
	case payloadrelease.ScopeEmulationStationImportItem:
		table = "emulationstation_import_items"
	case payloadrelease.ScopeImportItem, payloadrelease.ScopeImportJob, payloadrelease.ScopeUploadConsumption,
		payloadrelease.ScopeGame, payloadrelease.ScopeBlob:
		return nil, application.ErrInvalidBundle
	default:
		return nil, application.ErrInvalidBundle
	}
	rows, err := records.executor.QueryContext(ctx,
		"SELECT id FROM "+table+" WHERE payload_state='RETAINED' AND id>? ORDER BY id LIMIT ?", query.AfterID, query.Limit)
	if err != nil {
		return nil, fmt.Errorf("query restored retained payloads: %w", err)
	}
	defer func() { cleanup.Error("close restored payload owners", rows.Close()) }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan restored payload owner: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate restored payload owners: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close restored payload owners: %w", err)
	}
	return ids, nil
}
