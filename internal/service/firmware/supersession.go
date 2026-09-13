package firmware

import (
	"context"
	"fmt"
	"math"

	"retrom/internal/service/payloadrelease"
)

type SupersededInstallation struct {
	ID, RequirementID, BlobID string
	Version                   int64
}

type SupersessionReader interface {
	Current(context.Context, string) (SupersededInstallation, bool, error)
	Consumption(context.Context, string) (string, error)
}

type SupersessionWriter interface {
	Deactivate(context.Context, SupersededInstallation, int64) error
}

type SupersessionScope struct {
	Read    SupersessionReader
	Write   SupersessionWriter
	Payload payloadrelease.SchedulingScope
}

func SupersedeInScope(ctx context.Context, scope SupersessionScope, requirementID string, now int64) error {
	before, found, err := scope.Read.Current(ctx, requirementID)
	if err != nil {
		return fmt.Errorf("read active BIOS for replacement: %w", err)
	}
	if !found {
		return nil
	}
	if before.ID == "" || before.RequirementID != requirementID || before.BlobID == "" ||
		before.Version < 1 || before.Version == math.MaxInt64 {
		return ErrInvalid
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
