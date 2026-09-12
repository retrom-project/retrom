package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	biopersistence "retrom/internal/persistence/corevalidation"
	tagpersistence "retrom/internal/persistence/tagging"
	application "retrom/internal/service/libraryimport"
)

type (
	ReviewApprovals           struct{ database *sql.DB }
	reviewApprovalRecords     struct{ transaction *sql.Tx }
	approvalDependencyRecords struct{ executor dbexec.Executor }
)

func NewReviewApprovals(database *sql.DB) *ReviewApprovals {
	return &ReviewApprovals{database: database}
}

func (repository *ReviewApprovals) WithApproval(
	ctx context.Context, work func(application.ReviewApprovalScope) error,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin review approval: %w", err)
	}
	defer dbexec.Rollback(transaction)
	if err := work(BindReviewApproval(transaction)); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit review approval: %w", err)
	}
	return nil
}

func BindReviewApproval(transaction *sql.Tx) application.ReviewApprovalScope {
	records := reviewApprovalRecords{transaction: transaction}
	return application.ReviewApprovalScope{
		Reader: records, Media: records, Validation: BindReviewValidation(transaction),
		Dependencies: BindApprovalDependencies(transaction), Duplicates: BindContentDuplicates(transaction),
		Tags: tagpersistence.Bind(transaction), Games: records, Variants: records, Decisions: records,
		Bulk: records,
	}
}

func BindApprovalDependencies(executor dbexec.Executor) application.ApprovalDependencyScope {
	return application.ApprovalDependencyScope{
		Reader: approvalDependencyRecords{executor: executor}, BIOS: biopersistence.New(executor),
		Arcade: BindArcadeRelations(executor),
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
		return application.ErrInvalid
	}
	return nil
}

func (records reviewApprovalRecords) TransitionOwner(
	ctx context.Context, change application.ReviewOwnerTransition,
) error {
	return TransitionReviewOwners(ctx, records.transaction, change)
}

func (records reviewApprovalRecords) SchedulePayload(
	ctx context.Context, change application.ReviewPayloadRelease,
) error {
	return ScheduleReviewPayloads(ctx, records.transaction, change)
}
