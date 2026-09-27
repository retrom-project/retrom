package launch

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/launch"
)

type ProjectIndexes struct{ database dbapi.DB }

func NewProjectIndexes(database dbapi.DB) *ProjectIndexes { return &ProjectIndexes{database: database} }

func (repository *ProjectIndexes) ReadProjectIndex(
	ctx context.Context, reference application.ProjectIndexReference, authorize application.ConfigAuthorization,
) (application.ProjectIndexSnapshot, bool, error) {
	tx, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ProjectIndexSnapshot{}, false, fmt.Errorf("begin project index: %w", err)
	}
	defer dbapi.Rollback(tx)
	ref := application.SessionRef{ID: reference.ID, Preview: reference.PreviewOnly}
	source, found, err := configSource(ctx, tx, ref)
	if err != nil {
		return application.ProjectIndexSnapshot{}, false, err
	}
	if !found && !ref.Preview {
		ref.Preview = true
		source, found, err = configSource(ctx, tx, ref)
	}
	if err != nil || !found {
		return application.ProjectIndexSnapshot{}, false, err
	}
	if err := authorize(source); err != nil {
		return application.ProjectIndexSnapshot{}, false, err
	}
	files, err := projectIndexFiles(ctx, tx, ref)
	if err != nil {
		return application.ProjectIndexSnapshot{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return application.ProjectIndexSnapshot{}, false, fmt.Errorf("commit project index: %w", err)
	}
	return application.ProjectIndexSnapshot{Source: source, Files: files}, true, nil
}
