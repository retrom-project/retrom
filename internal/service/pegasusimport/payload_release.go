package pegasusimport

import (
	"context"
	"fmt"

	payload "retrom/internal/service/payloadrelease"
)

func scheduleTerminalPayloads(ctx context.Context, scope payload.ReleaseScope, id string, now int64) error {
	err := payload.NewScheduler(nil).TerminalSources(ctx, scope, payload.SourceBatch{
		Type: payload.ScopePegasusImportItem, ImportID: id,
	}, now)
	if err != nil {
		return fmt.Errorf("schedule terminal Pegasus payloads: %w", err)
	}
	return nil
}
