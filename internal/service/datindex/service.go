package datindex

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/datindex"
)

// SyncRequirements coordinates DAT reads and requirement writes. The model
// package supplies the value types and pure requirement builder; persistence
// remains behind the Records port.
func SyncRequirements(ctx context.Context, records model.Records, datID string, now time.Time) error {
	definition, err := records.Definition(ctx, datID)
	if err != nil {
		return fmt.Errorf("datindex/read definition: %w", err)
	}
	machines, err := records.MachineNames(ctx, datID)
	if err != nil {
		return fmt.Errorf("datindex/read machines: %w", err)
	}
	for _, machine := range machines {
		entries, err := records.RequiredEntries(ctx, datID, machine)
		if err != nil {
			return fmt.Errorf("datindex/read requirement entries: %w", err)
		}
		requirement, err := model.BuildRequirement(definition, datID, machine, entries, now.UnixMilli())
		if err != nil {
			return err
		}
		if err := records.UpsertRequirement(ctx, requirement); err != nil {
			return fmt.Errorf("datindex/sync requirement: %w", err)
		}
	}
	if err := records.DisableStale(ctx, model.Retirement{
		ProviderID: definition.ProviderID, TargetID: definition.TargetID,
		CurrentVersionID: datID, AtMS: now.UnixMilli(),
	}); err != nil {
		return fmt.Errorf("datindex/retire requirements: %w", err)
	}
	return nil
}
