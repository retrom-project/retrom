// Package composition binds application ports to concrete process dependencies.
package composition

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"retrom/internal/emulationstationimport"
	"retrom/internal/libraryimport"
	discardpersistence "retrom/internal/persistence/importdiscard"
	"retrom/internal/service/importdiscard"
	"retrom/internal/service/pegasusimport"
)

func NewImportDiscard(database *sql.DB, importer *libraryimport.Service, pegasus *pegasusimport.Service,
	emulationstation *emulationstationimport.Service, now func() time.Time,
) *importdiscard.Service {
	return importdiscard.New(
		discardpersistence.New(
			database,
		),
		discardImports{
			importer,
		},
		discardSources{
			pegasus,
			emulationstation,
		},
		now,
	)
}

type discardImports struct{ *libraryimport.Service }

func (imports discardImports) CancelForDiscard(ctx context.Context, id string, version int64) error {
	_, _, err := imports.Service.CancelForDiscard(ctx, id, version)
	if errors.Is(err, libraryimport.ErrInvalid) {
		return importdiscard.ErrNotCancellable
	}
	if err != nil {
		return fmt.Errorf("cancel library import for discard: %w", err)
	}
	return nil
}

type discardSources struct {
	pegasus          *pegasusimport.Service
	emulationstation *emulationstationimport.Service
}

func (sources discardSources) Cancel(ctx context.Context, kind, id string, version int64, reason, userID string) error {
	var err error
	switch kind {
	case "PEGASUS":
		_, _, err = sources.pegasus.Cancel(ctx, id, version, reason, userID)
	case "EMULATIONSTATION":
		_, _, err = sources.emulationstation.Cancel(ctx, id, version, reason, userID)
	default:
		return importdiscard.ErrInvalid
	}
	if errors.Is(err, pegasusimport.ErrNotCancellable) || errors.Is(err, emulationstationimport.ErrNotCancellable) {
		return importdiscard.ErrNotCancellable
	}
	if err != nil {
		return fmt.Errorf("cancel server source for discard: %w", err)
	}
	return nil
}
