package emulationstationimport

import (
	"context"
	"fmt"

	application "retrom/internal/model/emulationstationimport"
)

func (source *Sources) checkSourceExecution(ctx context.Context, unit application.Execution) error {
	if err := source.guard.Check(ctx, unit); err != nil {
		return fmt.Errorf("check EmulationStation source: %w", err)
	}
	return nil
}
