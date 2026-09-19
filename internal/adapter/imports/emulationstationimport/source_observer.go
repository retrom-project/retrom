package emulationstationimport

import (
	"context"
	"fmt"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
)

func (source *Sources) checkSourceExecution(ctx context.Context, unit emulationstationimportmodel.Execution) error {
	if err := source.guard.Check(ctx, unit); err != nil {
		return fmt.Errorf("check EmulationStation source: %w", err)
	}
	return nil
}
