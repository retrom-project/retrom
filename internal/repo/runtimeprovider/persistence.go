package runtimeprovider

import (
	"context"
	"database/sql"
	"fmt"

	service "retrom/internal/model/runtimeprovider"
	"retrom/internal/repo/dbexec"
)

type (
	Repository        struct{ database *sql.DB }
	catalogRecords    struct{ executor dbexec.Executor }
	projectionRecords struct{ executor dbexec.Executor }
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }
func (repository *Repository) CommitReconcile(
	ctx context.Context, cmd service.ReconcileCommand,
) error {
	err := dbexec.Immediate(ctx, repository.database, func(exec dbexec.Executor) error {
		catalog := catalogRecords{executor: exec}
		current, err := catalog.Current(ctx)
		if err != nil {
			return fmt.Errorf("read current runtime catalog: %w", err)
		}
		change, changed, err := prepareReconciliation(
			ctx, catalog, current, cmd.Candidate, cmd.NowMS,
		)
		if err != nil {
			return err
		}
		if len(changed) == 0 && current.CatalogSHA256 == cmd.Candidate.CatalogSHA256 {
			return nil
		}
		return applyChangedProjection(
			ctx, projectionRecords{executor: exec}, change, changed,
		)
	})
	if err != nil {
		return fmt.Errorf("reconcile runtime provider: %w", err)
	}
	return nil
}

func (records projectionRecords) Publish(ctx context.Context, input service.Publication) error {
	tx := records.executor
	if err := clearHostBindings(ctx, tx); err != nil {
		return err
	}
	if err := synchronizeDefinitions(ctx, tx, input.Candidate, input.AtMS); err != nil {
		return err
	}
	if err := writeProvidersAndTargets(ctx, tx, input.Candidate, input.AtMS); err != nil {
		return err
	}
	if err := removeStaleProjection(ctx, tx, input.RemovedProviders, input.RemovedTargets); err != nil {
		return err
	}
	if err := writeHostBindings(ctx, tx, input.Candidate.Bindings); err != nil {
		return err
	}
	return writeCatalogState(ctx, tx, input.Candidate, input.AtMS)
}

func (records projectionRecords) TerminateSessions(ctx context.Context, id string, now int64) error {
	return terminateProviderSessions(ctx, records.executor, id, now)
}

func (records projectionRecords) Audit(ctx context.Context, input service.Audit) error {
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO audit_events(
 id,actor_kind,actor_label,action,resource_type,resource_id,diff_json,created_at_ms
) VALUES(?,'SYSTEM','runtime-provider-reconciliation','RUNTIME_PROVIDER_RECONCILED',
 'RUNTIME_PROVIDER_CATALOG','active',?,?)
`, input.ID, string(input.DiffJSON), input.AtMS); err != nil {
		return fmt.Errorf("runtimeprovider/write reconciliation audit: %w", err)
	}
	return nil
}

func writeProvidersAndTargets(ctx context.Context, tx dbexec.Executor, candidate service.Projection, now int64) error {
	for _, provider := range candidate.Providers {
		if err := writeProvider(ctx, tx, provider, now); err != nil {
			return err
		}
		for _, target := range provider.Targets {
			if err := writeTarget(ctx, tx, provider.Active.ProviderID, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func removeStaleProjection(
	ctx context.Context,
	tx dbexec.Executor,
	providers []string,
	targets []service.TargetIdentity,
) error {
	for _, target := range targets {
		if _, err := tx.ExecContext(
			ctx,
			`DELETE FROM runtime_targets WHERE provider_id=? AND target_id=?`,
			target.ProviderID,
			target.TargetID,
		); err != nil {
			return fmt.Errorf("runtimeprovider/remove target: %w", err)
		}
	}
	for _, id := range providers {
		if _, err := tx.ExecContext(ctx, `DELETE FROM runtime_providers WHERE provider_id=?`, id); err != nil {
			return fmt.Errorf("runtimeprovider/remove provider: %w", err)
		}
	}
	return nil
}
