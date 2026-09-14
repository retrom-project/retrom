package launch

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/launch"
	"retrom/internal/repo/dbexec"
)

type ProjectIndexes struct{ database *sql.DB }

func NewProjectIndexes(database *sql.DB) *ProjectIndexes { return &ProjectIndexes{database: database} }

func (repository *ProjectIndexes) ReadProjectIndex(
	ctx context.Context, reference application.ProjectIndexReference, authorize application.ConfigAuthorization,
) (application.ProjectIndexSnapshot, bool, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ProjectIndexSnapshot{}, false, fmt.Errorf("begin project index: %w", err)
	}
	defer dbexec.Rollback(tx)
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
