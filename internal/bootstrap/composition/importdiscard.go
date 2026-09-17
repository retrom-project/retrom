// Package composition binds application ports to concrete process dependencies.
package composition

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	importdiscardmodel "retrom/internal/model/importdiscard"
	"retrom/internal/model/libraryimport"
	pegasusimportmodel "retrom/internal/model/pegasusimport"
	discardpersistence "retrom/internal/repo/importdiscard"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	importdiscardservice "retrom/internal/service/importdiscard"
	pegasusimportservice "retrom/internal/service/pegasusimport"
)

func NewImportDiscard(database *sql.DB, importer importdiscardmodel.ImportWorkflow, pegasus *pegasusimportservice.Service,
	emulationstation *emulationstationimportservice.Service, now func() time.Time,
) *importdiscardservice.Service {
	return importdiscardservice.New(
		discardpersistence.New(
			database,
		),
		discardImports{workflow: importer},
		discardSources{
			pegasus,
			emulationstation,
		},
		now,
	)
}

type discardImports struct {
	workflow importdiscardmodel.ImportWorkflow
}

func (imports discardImports) CancelForDiscard(ctx context.Context, id string, version int64) error {
	if imports.workflow == nil {
		return importdiscardmodel.ErrNotCancellable
	}
	err := imports.workflow.CancelForDiscard(ctx, id, version)
	if errors.Is(err, libraryimport.ErrInvalid) {
		return importdiscardmodel.ErrNotCancellable
	}
	if err != nil {
		return fmt.Errorf("cancel library import for discard: %w", err)
	}
	return nil
}

func (imports discardImports) DiscardBatchReviews(ctx context.Context, id string) (bool, error) {
	if imports.workflow == nil {
		return false, importdiscardmodel.ErrNotCancellable
	}
	discarded, err := imports.workflow.DiscardBatchReviews(ctx, id)
	if err != nil {
		return false, fmt.Errorf("discard library import reviews: %w", err)
	}
	return discarded, nil
}

func (imports discardImports) ReleaseDiscardedBatch(ctx context.Context, id string) error {
	if imports.workflow == nil {
		return importdiscardmodel.ErrNotCancellable
	}
	if err := imports.workflow.ReleaseDiscardedBatch(ctx, id); err != nil {
		return fmt.Errorf("release discarded library import: %w", err)
	}
	return nil
}

type discardSources struct {
	pegasus          *pegasusimportservice.Service
	emulationstation *emulationstationimportservice.Service
}

func (sources discardSources) Cancel(ctx context.Context, kind, id string, version int64, reason, userID string) error {
	var err error
	switch kind {
	case "PEGASUS":
		_, _, err = sources.pegasus.Cancel(ctx, id, version, reason, userID)
	case "EMULATIONSTATION":
		_, _, err = sources.emulationstation.Cancel(ctx, id, version, reason, userID)
	default:
		return importdiscardmodel.ErrInvalid
	}
	if errors.Is(err, pegasusimportmodel.ErrNotCancellable) || errors.Is(err, emulationstationimportmodel.ErrNotCancellable) {
		return importdiscardmodel.ErrNotCancellable
	}
	if err != nil {
		return fmt.Errorf("cancel server source for discard: %w", err)
	}
	return nil
}
