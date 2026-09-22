// Package composition binds application ports to concrete process dependencies.
package composition

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	discardpersistence "retrom/internal/persistence/importdiscard"
	"retrom/internal/service/importdiscard"
	"retrom/internal/service/libraryimport"
	"retrom/internal/service/sourceimport"
)

func NewImportDiscard(database *sql.DB, importer importdiscard.ImportWorkflow, source *sourceimport.Service,
	now func() time.Time,
) *importdiscard.Service {
	return importdiscard.New(
		discardpersistence.New(
			database,
		),
		discardImports{workflow: importer},
		discardSources{
			source,
		},
		now,
	)
}

type discardImports struct{ workflow importdiscard.ImportWorkflow }

func (imports discardImports) CancelForDiscard(ctx context.Context, id string, version int64) error {
	if imports.workflow == nil {
		return importdiscard.ErrNotCancellable
	}
	err := imports.workflow.CancelForDiscard(ctx, id, version)
	if errors.Is(err, libraryimport.ErrInvalid) {
		return importdiscard.ErrNotCancellable
	}
	if err != nil {
		return fmt.Errorf("cancel library import for discard: %w", err)
	}
	return nil
}

func (imports discardImports) DiscardBatchReviews(ctx context.Context, id string) (bool, error) {
	if imports.workflow == nil {
		return false, importdiscard.ErrNotCancellable
	}
	discarded, err := imports.workflow.DiscardBatchReviews(ctx, id)
	if err != nil {
		return false, fmt.Errorf("discard library import reviews: %w", err)
	}
	return discarded, nil
}

func (imports discardImports) ReleaseDiscardedBatch(ctx context.Context, id string) error {
	if imports.workflow == nil {
		return importdiscard.ErrNotCancellable
	}
	if err := imports.workflow.ReleaseDiscardedBatch(ctx, id); err != nil {
		return fmt.Errorf("release discarded library import: %w", err)
	}
	return nil
}

type discardSources struct {
	source *sourceimport.Service
}

func (sources discardSources) Cancel(ctx context.Context, kind, id string, version int64, reason, userID string) error {
	var err error
	switch kind {
	case "SOURCE":
		_, _, err = sources.source.Cancel(ctx, id, version, reason, userID)
	default:
		return importdiscard.ErrInvalid
	}
	if errors.Is(err, sourceimport.ErrNotCancellable) {
		return importdiscard.ErrNotCancellable
	}
	if err != nil {
		return fmt.Errorf("cancel server source for discard: %w", err)
	}
	return nil
}
