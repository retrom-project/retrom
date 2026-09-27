package payloadrelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	bios "retrom/internal/persistence/firmware/payloadbios"
	launch "retrom/internal/persistence/launch/payloadlaunch"
	application "retrom/internal/service/payloadrelease"
)

type Retirement struct{ database dbapi.DB }

func NewRetirement(database dbapi.DB) *Retirement { return &Retirement{database: database} }

func (repository *Retirement) WithRetirement(ctx context.Context, run func(application.RetirementScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin retirement transaction: %w", err)
	}
	defer dbapi.Rollback(tx)
	records := retirementRecords{bios: bios.Records{Executor: tx}, launch: launch.Records{Executor: tx}}
	if err := run(application.RetirementScope{Read: records, BIOS: records.bios, Launch: records.launch}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit retirement transaction: %w", err)
	}
	return nil
}

type retirementRecords struct {
	bios   bios.Records
	launch launch.Records
}

func (records retirementRecords) BIOS(ctx context.Context, limit int) (application.BIOSRetirement, error) {
	return wrapPair(records.bios.BIOS(ctx, limit))
}

func (records retirementRecords) Launch(ctx context.Context, now int64,
	limit int,
) (application.LaunchRetirement, error) {
	return wrapPair(records.launch.Launch(ctx, now, limit))
}
