package netplay

import (
	"context"
	"fmt"

	"retrom/internal/model/netplay"
	validation "retrom/internal/repo/corevalidation"
	"retrom/internal/repo/dbexec"
)

// CommitWrite is a test-only helper that provides direct WriteScope access
// for integration tests that need to inject failures or inspect intermediate state.
func (repository *RoomControl) CommitWrite(ctx context.Context, work func(netplay.RoomControlScope) error) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("netplay/begin room control: %w", err)
	}
	defer dbexec.Rollback(transaction)
	records := roomControlRecords{transaction}
	scope := netplay.RoomControlScope{
		Read:        records,
		Write:       records,
		Eligibility: NewEligibility(transaction),
		BIOS:        validation.New(transaction),
	}
	if err := work(scope); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("netplay/commit room control: %w", err)
	}
	return nil
}
