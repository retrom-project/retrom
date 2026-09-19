package emulationstationimport

import (
	"context"
	"fmt"

	payloadmodel "retrom/internal/model/payloadrelease"
	payloadrepo "retrom/internal/repo/payloadrelease"

	"retrom/internal/repo/dbexec"
)

func scheduleTerminalPayloads(ctx context.Context, executor dbexec.Executor, id string, now int64) error {
	scope := payloadrepo.BindReleases(executor)
	err := payloadrepo.NewScheduler(nil).TerminalSources(ctx, scope, payloadmodel.SourceBatch{
		Type: payloadmodel.ScopeEmulationStationImportItem, ImportID: id,
	}, now)
	if err != nil {
		return fmt.Errorf("schedule terminal EmulationStation payloads: %w", err)
	}
	return nil
}
