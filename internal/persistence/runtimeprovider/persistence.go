package runtimeprovider

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	service "retrom/internal/service/runtimeprovider"
)

type (
	Repository        struct{ database dbapi.DB }
	catalogRecords    struct{ executor dbapi.Executor }
	projectionRecords struct{ transaction dbapi.Tx }
)

func New(database dbapi.DB) *Repository { return &Repository{database: database} }
func (repository *Repository) WithWrite(ctx context.Context, work func(service.WriteScope) error) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		if err := work(
			service.WriteScope{
				Catalog: catalogRecords{
					executor: tx,
				},
				Projection: projectionRecords{
					transaction: tx,
				},
			},
		); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("runtimeprovider/commit: %w", err)
	}
	return nil
}

func (records projectionRecords) Publish(ctx context.Context, input service.Publication) error {
	tx := records.transaction
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

func (records projectionRecords) Audit(ctx context.Context, input service.Audit) error {
	if _, err := records.transaction.ExecContext(ctx, `
INSERT INTO audit_events(
 id,actor_kind,actor_label,action,resource_type,resource_id,diff_json,created_at_ms
) VALUES(?,'SYSTEM','runtime-provider-reconciliation','RUNTIME_PROVIDER_RECONCILED',
 'RUNTIME_PROVIDER_CATALOG','active',?,?)
`, input.ID, string(input.DiffJSON), input.AtMS); err != nil {
		return fmt.Errorf("runtimeprovider/write reconciliation audit: %w", err)
	}
	return nil
}

func writeProvidersAndTargets(ctx context.Context, tx dbapi.Tx, candidate service.Projection, now int64) error {
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
	tx dbapi.Tx,
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
