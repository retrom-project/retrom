package dependencies

import (
	"context"
	"fmt"
	"time"

	"retrom/internal/adapter/runtime/dependencies"
	"retrom/internal/capability/format/arcadedat"
	"retrom/internal/capability/security/authn"
	"retrom/internal/service/datindex"

	"github.com/google/uuid"
)

func publishBuiltInDATCatalog(
	ctx context.Context, repository Repository, datID, jobID string, indexed, expected int64,
	catalog arcadedat.Catalog, now time.Time,
) error {
	err := repository.CommitWrite(ctx, func(scope WriteScope) error {
		if err := scope.Catalog.Publish(
			ctx,
			CatalogPublication{
				DATID:   datID,
				Catalog: catalog,
				Replace: indexed != expected,
				AtMS:    now.UnixMilli(),
			},
		); err != nil {
			return fmt.Errorf("publish DAT index: %w", err)
		}
		if err := activateBuiltInDAT(ctx, scope, datID, now); err != nil {
			return err
		}
		event := []byte(
			fmt.Sprintf(
				`{"schemaVersion":1,"executionNo":1,"attempt":1,"machineCount":%d}`,
				catalog.Stats.MachineCount,
			),
		)
		if err := scope.Jobs.Finish(
			ctx,
			JobFinish{
				JobID: jobID,
				DATID: datID,
				State: "SUCCEEDED",
				AtMS:  now.UnixMilli(),
				Event: event,
			},
		); err != nil {
			return fmt.Errorf("finish DAT publication: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("publish built-in DAT: %w", err)
	}
	return nil
}

func activateBuiltInDAT(ctx context.Context, scope WriteScope, datID string, now time.Time) error {
	state, err := scope.DAT.Activation(ctx, datID)
	if err != nil {
		return fmt.Errorf("inspect DAT activation: %w", err)
	}
	if state.ParseStatus != "READY" {
		return fmt.Errorf("%w: selected DAT is not ready", dependencies.ErrInvalid)
	}
	if state.Active {
		return nil
	}
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("create activation audit ID: %w", err)
	}
	if err := scope.DAT.Select(ctx, DATSelection{
		ID: datID, Target: state.Target, AtMS: now.UnixMilli(), AuditID: id.String(),
		Actor: authn.ActorFromContext(ctx, "release-setup"),
	}); err != nil {
		return fmt.Errorf("select built-in DAT: %w", err)
	}
	if err := datindex.SyncRequirements(ctx, scope.Requirements, datID, now); err != nil {
		return fmt.Errorf("sync DAT requirements: %w", err)
	}
	return nil
}
