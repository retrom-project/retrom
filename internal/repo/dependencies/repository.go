package dependencies

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	service "retrom/internal/model/dependencies"
	"retrom/internal/repo/datindex"
	"retrom/internal/repo/dbexec"
)

var errDATParseFailed = errors.New("DEPENDENCY_DAT_PARSE_FAILED")

type (
	Repository     struct{ database *sql.DB }
	targetRecords  struct{ executor dbexec.Executor }
	biosRecords    struct{ executor dbexec.Executor }
	datRecords     struct{ executor dbexec.Executor }
	catalogRecords struct{ transaction *sql.Tx }
	jobRecords     struct{ executor dbexec.Executor }
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) beginScope(ctx context.Context) (*sql.Tx, service.WriteScope, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, service.WriteScope{}, fmt.Errorf("dependencies/begin: %w", err)
	}
	scope := service.WriteScope{
		Targets: targetRecords{executor: tx}, BIOS: biosRecords{executor: tx},
		DAT:          datRecords{executor: tx},
		Catalog:      catalogRecords{transaction: tx},
		Jobs:         jobRecords{executor: tx},
		Requirements: datindex.Bind(tx),
	}
	return tx, scope, nil
}

func commitScope(tx *sql.Tx) error {
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("dependencies/commit: %w", err)
	}
	return nil
}

func (repository *Repository) CommitBootstrapDefinitions(
	ctx context.Context, cmd service.BootstrapCommand,
) error {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return err
	}
	defer dbexec.Rollback(tx)
	if err := bootstrapBIOS(ctx, scope, cmd.BIOSEntries); err != nil {
		return err
	}
	if err := bootstrapDATs(ctx, scope, cmd.DATEntries, cmd.NowMS); err != nil {
		return err
	}
	return commitScope(tx)
}

func bootstrapBIOS(ctx context.Context, scope service.WriteScope, entries []service.BIOSRequirement) error {
	for _, req := range entries {
		exists, err := scope.Targets.Exists(ctx, service.RuntimeTarget{
			ProviderID: req.ProviderID, TargetID: req.TargetID,
		})
		if err != nil {
			return fmt.Errorf("dependencies/read target: %w", err)
		}
		if !exists {
			continue
		}
		if err := scope.BIOS.Upsert(ctx, req); err != nil {
			return fmt.Errorf("dependencies/seed BIOS: %w", err)
		}
	}
	return nil
}

func bootstrapDATs(
	ctx context.Context, scope service.WriteScope, entries []service.DATBootstrapEntry, nowMS int64,
) error {
	for _, entry := range entries {
		if err := bootstrapSingleDAT(ctx, scope, entry, nowMS); err != nil {
			return err
		}
	}
	return nil
}

func bootstrapSingleDAT(
	ctx context.Context, scope service.WriteScope, entry service.DATBootstrapEntry, nowMS int64,
) error {
	exists, err := scope.Targets.Exists(ctx, entry.Registration.Target)
	if err != nil {
		return fmt.Errorf("dependencies/read target: %w", err)
	}
	if !exists {
		return nil
	}
	registered, err := scope.DAT.Register(ctx, entry.Registration)
	if err != nil {
		return fmt.Errorf("dependencies/register DAT: %w", err)
	}
	if registered.ParseStatus == "READY" && registered.Stats != entry.Expected {
		if err := scope.DAT.Reset(ctx, registered.ID, nowMS); err != nil {
			return fmt.Errorf("dependencies/reset DAT: %w", err)
		}
	}
	if entry.Preferred {
		if err := scope.DAT.Retire(ctx, entry.Registration.Target, registered.ID, nowMS); err != nil {
			return fmt.Errorf("dependencies/retire DAT: %w", err)
		}
	}
	return nil
}

func (repository *Repository) CommitEnsureDATJob(
	ctx context.Context, cmd service.EnsureDATJobCommand,
) (string, error) {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return "", err
	}
	defer dbexec.Rollback(tx)
	job, found, err := scope.Jobs.Find(ctx, cmd.DedupeKey)
	if err != nil {
		return "", fmt.Errorf("dependencies/find job: %w", err)
	}
	if found {
		id, err := recoverExistingJob(ctx, scope, job, cmd.NowMS)
		if err != nil {
			return "", err
		}
		return id, commitScope(tx)
	}
	version, err := scope.Catalog.Version(ctx, cmd.DATID)
	if err != nil {
		return "", fmt.Errorf("dependencies/read DAT version: %w", err)
	}
	creation, err := buildDATJobCreation(cmd, version)
	if err != nil {
		return "", err
	}
	if err := scope.Jobs.Create(ctx, creation); err != nil {
		return "", fmt.Errorf("dependencies/create job: %w", err)
	}
	if err := commitScope(tx); err != nil {
		return "", err
	}
	return creation.ID, nil
}

func recoverExistingJob(ctx context.Context, scope service.WriteScope, job service.Job, nowMS int64) (string, error) {
	if job.State == "FAILED" || job.State == "CANCELLED" {
		return "", fmt.Errorf("dependencies/DAT state: %w", errDATParseFailed)
	}
	if job.State == "RUNNING" {
		if err := scope.Jobs.Requeue(ctx, job.ID, nowMS); err != nil {
			return "", fmt.Errorf("dependencies/recover job: %w", err)
		}
	}
	return job.ID, nil
}

func (repository *Repository) CommitClaimDAT(
	ctx context.Context, cmd service.ClaimDATCommand,
) error {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return err
	}
	defer dbexec.Rollback(tx)
	if err := scope.Jobs.Claim(ctx, cmd.Claim); err != nil {
		return fmt.Errorf("dependencies/claim job: %w", err)
	}
	if err := scope.Catalog.MarkParsing(ctx, cmd.MarkDAT, cmd.Claim.AtMS); err != nil {
		return fmt.Errorf("dependencies/mark parsing: %w", err)
	}
	return commitScope(tx)
}

func (repository *Repository) CommitFailDAT(
	ctx context.Context, cmd service.FailDATCommand,
) error {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return err
	}
	defer dbexec.Rollback(tx)
	if err := scope.Catalog.MarkFailed(ctx, cmd.DATID, cmd.Finish.AtMS); err != nil {
		return fmt.Errorf("dependencies/mark failure: %w", err)
	}
	if err := scope.Jobs.Finish(ctx, cmd.Finish); err != nil {
		return fmt.Errorf("dependencies/finish failed job: %w", err)
	}
	return commitScope(tx)
}

func (repository *Repository) CommitActivateDAT(
	ctx context.Context, cmd service.ActivateDATCommand,
) error {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return err
	}
	defer dbexec.Rollback(tx)
	if err := activateDAT(ctx, scope, cmd); err != nil {
		return err
	}
	return commitScope(tx)
}

func (repository *Repository) CommitPublishDAT(
	ctx context.Context, cmd service.PublishDATCommand,
) error {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return err
	}
	defer dbexec.Rollback(tx)
	if err := scope.Catalog.Publish(ctx, cmd.Publication); err != nil {
		return fmt.Errorf("dependencies/publish: %w", err)
	}
	if err := activateDAT(ctx, scope, cmd.Activation); err != nil {
		return err
	}
	if err := scope.Jobs.Finish(ctx, cmd.Finish); err != nil {
		return fmt.Errorf("dependencies/finish publication: %w", err)
	}
	return commitScope(tx)
}

func (repository *Repository) TargetExists(ctx context.Context, target service.RuntimeTarget) (bool, error) {
	return targetRecords{executor: repository.database}.Exists(ctx, target)
}

func (records targetRecords) Exists(ctx context.Context, target service.RuntimeTarget) (bool, error) {
	var found int
	err := records.executor.QueryRowContext(
		ctx,
		`SELECT 1 FROM runtime_targets WHERE provider_id=? AND target_id=?`,
		target.ProviderID,
		target.TargetID,
	).Scan(
		&found,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("dependencies/read runtime target: %w", err)
	}
	return true, nil
}

func (repository *Repository) FindDAT(ctx context.Context, lookup service.DATLookup) (service.DATState, error) {
	var state service.DATState
	err := repository.database.QueryRowContext(ctx, `
SELECT d.id,
d.parse_status,
(SELECT count(*)
FROM dat_machines m
WHERE m.dat_version_id=d.id)
FROM dat_versions d
WHERE d.sha256=?
AND d.parser_version='retrom-dat-v1'
AND d.core_id=?
AND d.provider_id=?
AND d.target_id=?
`, lookup.SHA256, lookup.CoreID, lookup.Target.ProviderID, lookup.Target.TargetID).
		Scan(&state.ID, &state.ParseStatus, &state.Indexed)
	if err != nil {
		return service.DATState{}, fmt.Errorf("dependencies/find DAT index: %w", err)
	}
	return state, nil
}
