package launch

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/launch"
	"retrom/internal/repo/dbexec"
)

func ProductContentSnapshot(
	ctx context.Context,
	executor dbexec.Executor,
	source application.ProductSource,
) (application.ProductSnapshot, error) {
	game, err := productCreationFiles(ctx, executor, source.GameID, false)
	if err != nil {
		return application.ProductSnapshot{}, err
	}
	variant, err := productCreationFiles(ctx, executor, source.VariantID, true)
	if err != nil {
		return application.ProductSnapshot{}, err
	}
	return application.ProductSnapshot{Found: true, Source: source, GameFiles: game, VariantFiles: variant}, nil
}

func (repository *ProductCreation) Content(
	ctx context.Context,
	source application.ProductSource,
) (application.ProductSnapshot, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ProductSnapshot{}, fmt.Errorf("begin product content snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	snapshot, err := ProductContentSnapshot(ctx, tx, source)
	if err != nil {
		return application.ProductSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.ProductSnapshot{}, fmt.Errorf("commit product content snapshot: %w", err)
	}
	return snapshot, nil
}
