package payloadrelease

import (
	"context"

	uploads "retrom/internal/persistence/uploads/payloadpurge"
	application "retrom/internal/service/payloadrelease"
)

func (records effectRecords) Candidates(ctx context.Context, sessionID, cursor string,
	limit int,
) ([]application.EffectUpload, error) {
	return wrapPair((uploads.Records{Executor: records.executor}).Candidates(ctx, sessionID, cursor, limit))
}

func (records effectRecords) Purge(ctx context.Context, file application.EffectUpload, now int64) error {
	return wrapErr((uploads.Records{Executor: records.executor}).Purge(ctx, file, now))
}
