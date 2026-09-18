package pegasusimport

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/pegasusimport"
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

func (repository *Companions) readRecords(
	ctx context.Context,
) (companionRecords, *sql.Tx, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return companionRecords{}, nil, fmt.Errorf(
			"begin Pegasus companion read: %w", err,
		)
	}
	return companionRecords{tx: tx, executor: tx}, tx, nil
}

func (repository *Companions) LoadCompanionOwner(
	ctx context.Context, itemID string,
) (application.OwnedItem, error) {
	records, tx, err := repository.readRecords(ctx)
	if err != nil {
		return application.OwnedItem{}, err
	}
	defer dbexec.Rollback(tx)
	result, err := records.Owner(ctx, itemID)
	if err != nil {
		return application.OwnedItem{}, err
	}
	_ = tx.Commit()
	return result, nil
}

func (repository *Companions) LoadDependencies(
	ctx context.Context, datVersionID, machine string,
) ([]string, error) {
	records, tx, err := repository.readRecords(ctx)
	if err != nil {
		return nil, err
	}
	defer dbexec.Rollback(tx)
	result, err := records.Dependencies(ctx, datVersionID, machine)
	if err != nil {
		return nil, err
	}
	_ = tx.Commit()
	return result, nil
}

func (repository *Companions) LoadCandidates(
	ctx context.Context, item application.ExecutionItem,
) ([]application.CompanionCandidate, error) {
	records, tx, err := repository.readRecords(ctx)
	if err != nil {
		return nil, err
	}
	defer dbexec.Rollback(tx)
	result, err := records.Candidates(ctx, item)
	if err != nil {
		return nil, err
	}
	_ = tx.Commit()
	return result, nil
}

func (repository *Companions) CommitCompanionRegistration(
	ctx context.Context, change application.CompanionRegistration,
) (string, error) {
	return commitResultTx(ctx, repository.database, repository.preCommitHook,
		"Pegasus companion registration",
		func(tx *sql.Tx) (string, error) {
			return companionRecords{tx: tx, executor: tx}.Register(ctx, change)
		},
	)
}

type companionRecords struct {
	tx       *sql.Tx
	executor dbexec.Executor
}

func (records companionRecords) Owner(
	ctx context.Context, itemID string,
) (application.OwnedItem, error) {
	before, err := itemWorkRecords{tx: records.tx}.Current(ctx, itemID)
	if err != nil {
		return application.OwnedItem{}, err
	}
	if err := records.executor.QueryRowContext(ctx,
		`SELECT collection.target_platform_instance_id,collection.target_platform_id,`+
			`COALESCE(collection.target_dat_version_id,'') FROM pegasus_import_items item`+
			` JOIN pegasus_import_collections collection ON collection.id=item.collection_id`+
			` WHERE item.id=? AND collection.mapping_action='IMPORT'`, itemID).Scan(
		&before.Item.TargetPlatformID,
		&before.Item.TargetPlatformKind,
		&before.Item.TargetDATVersionID,
	); err != nil {
		return application.OwnedItem{}, fmt.Errorf("read Pegasus companion target: %w", err)
	}
	before.Item.Files, err = itemWorkRecords{tx: records.tx}.files(ctx, itemID)
	if err != nil {
		return application.OwnedItem{}, err
	}
	return before, nil
}

func (records companionRecords) Register(
	ctx context.Context,
	change application.CompanionRegistration,
) (string, error) {
	var valid bool
	if err := records.executor.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM pegasus_import_items item WHERE`+
			` item.id=? AND item.import_id=? AND item.version=? AND item.execution_state=?`+
			` AND item.execution_state='COPYING'`+
			itemExecutionFence+`)`,
		itemFenceArgs(change.Before, change.NowMS)...).Scan(&valid); err != nil {
		return "", fmt.Errorf("check Pegasus companion owner: %w", err)
	}
	if !valid {
		return "", application.ErrVersionConflict
	}
	candidates, err := records.Candidates(ctx, change.Before.Item)
	if err != nil {
		return "", err
	}
	valid = false
	for _, candidate := range candidates {
		if candidate == change.Candidate {
			valid = true
			break
		}
	}
	if !valid {
		return "", application.ErrVersionConflict
	}
	return registerVerifiedMaterial(
		ctx, records.executor, change.Blob, "application/zip", change.NowMS,
	)
}
