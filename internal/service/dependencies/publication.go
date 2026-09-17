package dependencies

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/dependencies"

	"retrom/internal/capability/format/arcadedat"

	"github.com/google/uuid"
)

func publishBuiltInDATCatalog(
	ctx context.Context, repository model.Repository, datID, jobID string, indexed, expected int64,
	catalog arcadedat.Catalog, now time.Time,
) error {
	auditID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("create publication audit ID: %w", err)
	}
	event := []byte(
		fmt.Sprintf(
			`{"schemaVersion":1,"executionNo":1,"attempt":1,"machineCount":%d}`,
			catalog.Stats.MachineCount,
		),
	)
	if err = repository.CommitPublishDAT(ctx, model.PublishDATCommand{
		Publication: model.CatalogPublication{
			DATID: datID, Catalog: catalog, Replace: indexed != expected, AtMS: now.UnixMilli(),
		},
		Activation: model.ActivateDATCommand{
			DATID: datID, AuditID: auditID.String(), NowMS: now.UnixMilli(),
		},
		Finish: model.JobFinish{
			JobID: jobID, DATID: datID, State: "SUCCEEDED", AtMS: now.UnixMilli(), Event: event,
		},
	}); err != nil {
		return fmt.Errorf("publish DAT catalog: %w", err)
	}
	return nil
}
