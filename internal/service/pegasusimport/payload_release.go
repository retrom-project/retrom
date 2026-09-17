package pegasusimport

import (
	"context"
	"fmt"

	payloadreleasemodel "retrom/internal/model/payloadrelease"
	payloadreleaseservice "retrom/internal/service/payloadrelease"
)

func scheduleTerminalPayloads(ctx context.Context, scope payloadreleasemodel.ReleaseScope, id string, now int64) error {
	err := payloadreleaseservice.NewScheduler(nil).TerminalSources(ctx, scope, payloadreleasemodel.SourceBatch{
		Type: payloadreleasemodel.ScopePegasusImportItem, ImportID: id,
	}, now)
	if err != nil {
		return fmt.Errorf("schedule terminal Pegasus payloads: %w", err)
	}
	return nil
}
