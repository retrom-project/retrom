package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/payloadrelease"
)

func (records effectRecords) Payload(ctx context.Context, scope application.Scope) (application.EffectPayload, error) {
	var payload application.EffectPayload
	var err error
	switch scope.Type {
	case application.ScopeGame:
		payload.BlobIDs, err = GameBlobIDs(ctx, records.executor, scope.ID)
		if err == nil {
			payload.Consumptions, err = records.gameConsumptions(ctx, scope.ID)
		}
	case application.ScopeImportItem:
		payload.BlobIDs, err = ImportItemBlobIDs(ctx, records.executor, scope.ID)
		if err == nil {
			payload.Consumptions, err = records.itemConsumptions(ctx, scope.ID)
		}
	case application.ScopeImportJob:
		payload.Consumptions, err = records.readConsumptions(
			ctx,
			`SELECT id,upload_session_id,COALESCE(upload_file_id,''),consumer_type,consumer_id,version,released_at_ms
FROM upload_consumptions WHERE consumer_type='IMPORT_JOB' AND consumer_id=?`,
			scope.ID,
		)
	case application.ScopePegasusImportItem, application.ScopeEmulationStationImportItem:
		spec, specErr := effectSourceSpec(scope.Type)
		if specErr != nil {
			return payload, specErr
		}
		payload.BlobIDs, err = collectIDs(
			ctx,
			records.executor,
			`SELECT blob_id FROM `+spec.filesTable+` WHERE item_id=?
UNION ALL SELECT source_archive_blob_id FROM `+spec.filesTable+` WHERE item_id=?
UNION ALL SELECT blob_id FROM `+spec.assetsTable+` WHERE item_id=?`,
			scope.ID,
			scope.ID,
			scope.ID,
		)
	case application.ScopeUploadConsumption, application.ScopeBlob:
		return payload, application.ErrScopeInvalid
	default:
		return payload, application.ErrScopeInvalid
	}
	if err != nil {
		return application.EffectPayload{}, fmt.Errorf("read release payload facts: %w", err)
	}
	return payload, nil
}

func (records effectRecords) readConsumptions(
	ctx context.Context,
	query string,
	args ...any,
) ([]application.EffectConsumption, error) {
	rows, err := records.executor.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query release consumptions: %w", err)
	}
	defer func() { cleanup.Error("close release consumptions", rows.Close()) }()
	values := make([]application.EffectConsumption, 0)
	for rows.Next() {
		var value application.EffectConsumption
		var released sql.NullInt64
		if err := rows.Scan(
			&value.ID,
			&value.SessionID,
			&value.FileID,
			&value.ConsumerType,
			&value.ConsumerID,
			&value.Version,
			&released,
		); err != nil {
			return nil, fmt.Errorf("decode release consumption: %w", err)
		}
		value.Released = application.WorkTime{Set: released.Valid, Value: released.Int64}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate release consumptions: %w", err)
	}
	return values, nil
}
