package payloadrelease

import (
	"context"
	"fmt"

	gamerelease "retrom/internal/persistence/gamecontent/gamerelease"
	gamerefs "retrom/internal/persistence/gamecontent/references"
	itemrelease "retrom/internal/persistence/libraryimport/itemrelease"
	importrefs "retrom/internal/persistence/libraryimport/references"
	sourcerelease "retrom/internal/persistence/sourceimport/sourcerelease"
	uploads "retrom/internal/persistence/uploads/payloadpurge"
	application "retrom/internal/service/payloadrelease"
)

func (records effectRecords) Payload(ctx context.Context, scope application.Scope) (application.EffectPayload, error) {
	var payload application.EffectPayload
	var err error
	switch scope.Type {
	case application.ScopeGame:
		payload.BlobIDs, err = gamerefs.GameBlobIDs(ctx, records.executor, scope.ID)
		if err == nil {
			payload.Consumptions, err = gamerelease.Consumptions(ctx, records.executor, scope.ID)
		}
	case application.ScopeImportItem:
		payload.BlobIDs, err = importrefs.ImportItemBlobIDs(ctx, records.executor, scope.ID)
		if err == nil {
			payload.Consumptions, err = itemrelease.Consumptions(ctx, records.executor, scope.ID)
		}
	case application.ScopeImportJob:
		payload.Consumptions, err = uploads.JobConsumptions(ctx, records.executor, scope.ID)
	case application.ScopeSourceImportItem:
		payload.BlobIDs, err = sourcerelease.BlobIDs(ctx, records.executor, scope.ID)
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
