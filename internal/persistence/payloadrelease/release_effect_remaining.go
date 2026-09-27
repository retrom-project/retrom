package payloadrelease

import (
	"context"

	gamerelease "retrom/internal/persistence/gamecontent/gamerelease"
	itemrelease "retrom/internal/persistence/libraryimport/itemrelease"
	sourcerelease "retrom/internal/persistence/sourceimport/sourcerelease"

	application "retrom/internal/service/payloadrelease"
)

func (records effectRecords) Remaining(ctx context.Context, scope application.Scope) (int64, error) {
	switch scope.Type {
	case application.ScopeGame:
		return wrapPair((gamerelease.Records{Executor: records.executor}).Remaining(ctx, scope.ID))
	case application.ScopeImportItem:
		return wrapPair((itemrelease.Records{Executor: records.executor}).ItemRemaining(ctx, scope.ID))
	case application.ScopeImportJob:
		return wrapPair((itemrelease.Records{Executor: records.executor}).JobRemaining(ctx, scope.ID))
	case application.ScopeSourceImportItem:
		return wrapPair(sourcerelease.Remaining(ctx, records.executor, scope.ID))
	case application.ScopeUploadConsumption, application.ScopeBlob:
		return 0, application.ErrScopeInvalid
	default:
		return 0, application.ErrScopeInvalid
	}
}
