package sourceimport

import (
	"context"
	"fmt"

	payload "retrom/internal/service/payloadrelease"
)

func scheduleTerminalPayloads(ctx context.Context, scope payload.ReleaseScope, id string, now int64) error {
	err := payload.NewScheduler(nil).TerminalSources(ctx, scope, payload.SourceBatch{
		Type: payload.ScopeSourceImportItem, ImportID: id,
	}, now)
	if err != nil {
		return fmt.Errorf("schedule terminal Source payloads: %w", err)
	}
	return nil
}
