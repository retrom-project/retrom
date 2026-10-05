package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	arcaderecords "retrom/internal/persistence/arcade"

	payloadpersistence "retrom/internal/persistence/libraryimport/itemrelease"

	dbapi "retrom/internal/database"
	biopersistence "retrom/internal/persistence/corevalidation"
	tagpersistence "retrom/internal/persistence/tagging"
	libraryservice "retrom/internal/service/libraryimport"
)

type (
	ReviewApprovals           struct{ database dbapi.DB }
	reviewApprovalRecords     struct{ transaction dbapi.Tx }
	approvalDependencyRecords struct{ executor dbapi.Executor }
)

func NewReviewApprovals(database dbapi.DB) *ReviewApprovals {
	return &ReviewApprovals{database: database}
}

func (repository *ReviewApprovals) WithApproval(
	ctx context.Context, work func(libraryservice.ReviewApprovalScope) error,
) error {
	// The filesystem publication runs between the two replayable database phases.
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		return work(BindReviewApproval(tx))
	})
	if err != nil {
		return fmt.Errorf("commit review approval: %w", err)
	}
	return nil
}

func BindReviewApproval(transaction dbapi.Tx) libraryservice.ReviewApprovalScope {
	records := reviewApprovalRecords{transaction: transaction}
	return libraryservice.ReviewApprovalScope{
		Publications: records, Payload: payloadpersistence.BindScheduling(transaction),
		Reader: records, Media: records, Profiles: BindReviewInputs(transaction),
		Dependencies: BindApprovalDependencies(transaction), Duplicates: BindContentDuplicates(transaction),
		Tags: tagpersistence.Bind(transaction), Games: records, Variants: records, Decisions: records,
		Bulk: records,
	}
}

func BindApprovalDependencies(executor dbapi.Executor) libraryservice.ApprovalDependencyScope {
	return libraryservice.ApprovalDependencyScope{
		Reader: approvalDependencyRecords{executor: executor}, BIOS: biopersistence.New(executor),
		Arcade: arcaderecords.New(executor),
	}
}

func approvalMutation(result sql.Result, err error, action string, exactlyOne bool) error {
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s result: %w", action, err)
	}
	if exactlyOne && changed != 1 {
		return libraryservice.ErrInvalid
	}
	return nil
}

func (records reviewApprovalRecords) TransitionOwner(
	ctx context.Context, change libraryservice.ReviewOwnerTransition,
) error {
	return TransitionReviewOwners(ctx, records.transaction, change)
}
