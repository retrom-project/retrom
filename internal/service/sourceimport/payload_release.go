package sourceimport

import (
	"context"
	"fmt"

	sourcecleanup "retrom/internal/service/sourceimport/payloadpolicy"

	payload "retrom/internal/service/cleanupjobs"
)

func scheduleTerminalPayloads(ctx context.Context, scope sourcecleanup.ReleaseScope, id string, now int64) error {
	err := sourcecleanup.TerminalSources(ctx, payload.NewScheduler(nil), scope, sourcecleanup.SourceBatch{
		Type: payload.ScopeSourceImportItem, ImportID: id,
	}, now)
	if err != nil {
		return fmt.Errorf("schedule terminal Source payloads: %w", err)
	}
	return nil
}
