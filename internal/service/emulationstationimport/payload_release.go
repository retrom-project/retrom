package emulationstationimport

import (
	"context"
	"fmt"

	payloadreleasemodel "retrom/internal/model/payloadrelease"
	payload "retrom/internal/service/payloadrelease"
)

func scheduleTerminalPayloads(ctx context.Context, scope payloadreleasemodel.ReleaseScope, id string, now int64) error {
	err := payload.NewScheduler(nil).TerminalSources(ctx, scope, payloadreleasemodel.SourceBatch{
		Type: payloadreleasemodel.ScopeEmulationStationImportItem, ImportID: id,
	}, now)
	if err != nil {
		return fmt.Errorf("schedule terminal EmulationStation payloads: %w", err)
	}
	return nil
}
