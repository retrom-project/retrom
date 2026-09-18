package emulationstationimport

import (
	"context"
	"database/sql"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
)

type Companions struct {
	database      *sql.DB
	preCommitHook func(dbexec.Executor) error
}

func NewCompanions(database *sql.DB) *Companions {
	return &Companions{database: database}
}

func (repository *Companions) WithPreCommitHook(
	hook func(dbexec.Executor) error,
) {
	repository.preCommitHook = hook
}

func (repository *Companions) LoadCompanionOwner(
	ctx context.Context, id string,
) (application.CompanionOwner, error) {
	return companionRecords{executor: repository.database}.Owner(ctx, id)
}

func (repository *Companions) LoadMappingTarget(
	ctx context.Context, id string,
) (application.MappingTarget, bool, error) {
	return companionRecords{executor: repository.database}.Target(ctx, id)
}

func (repository *Companions) LoadDependencies(
	ctx context.Context, datVersionID, machine string,
) ([]string, error) {
	return companionRecords{executor: repository.database}.Dependencies(
		ctx, datVersionID, machine,
	)
}

func (repository *Companions) LoadCandidates(
	ctx context.Context, owner application.CompanionOwner,
) ([]application.CompanionFile, error) {
	return companionRecords{executor: repository.database}.Candidates(ctx, owner)
}

func (repository *Companions) CommitCompanionBinding(
	ctx context.Context, change application.CompanionBinding,
) (string, error) {
	return commitResultTx(ctx, repository.database, repository.preCommitHook,
		"EmulationStation companion binding",
		func(tx *sql.Tx) (string, error) {
			return companionRecords{transaction: tx, executor: tx}.Register(ctx, change)
		},
	)
}

type companionRecords struct {
	transaction *sql.Tx
	executor    dbexec.Executor
}
