package firmware

import (
	"context"
	"fmt"
	"math"
	model "retrom/internal/model/firmware"

	"retrom/internal/service/payloadrelease"
)

func SupersedeInScope(ctx context.Context, scope model.SupersessionScope, requirementID string, now int64) error {
	before, found, err := scope.Read.Current(ctx, requirementID)
	if err != nil {
		return fmt.Errorf("read active BIOS for replacement: %w", err)
	}
	if !found {
		return nil
	}
	if before.ID == "" || before.RequirementID != requirementID || before.BlobID == "" ||
		before.Version < 1 || before.Version == math.MaxInt64 {
		return model.ErrInvalid
	}
	if err := scope.Write.Deactivate(ctx, before, now); err != nil {
		return fmt.Errorf("supersede active BIOS: %w", err)
	}
	consumptionID, err := scope.Read.Consumption(ctx, before.ID)
	if err != nil {
		return fmt.Errorf("read superseded BIOS consumption: %w", err)
	}
	if consumptionID == "" {
		return nil
	}
	if _, err := payloadrelease.NewScheduler(nil).Consumption(ctx, scope.Payload, consumptionID, now); err != nil {
		return fmt.Errorf("schedule superseded BIOS consumption: %w", err)
	}
	return nil
}
