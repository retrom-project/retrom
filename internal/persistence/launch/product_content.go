package launch

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/launch"
)

func ProductContentSnapshot(
	ctx context.Context,
	executor dbapi.Executor,
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
	tx, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ProductSnapshot{}, fmt.Errorf("begin product content snapshot: %w", err)
	}
	defer dbapi.Rollback(tx)
	snapshot, err := ProductContentSnapshot(ctx, tx, source)
	if err != nil {
		return application.ProductSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.ProductSnapshot{}, fmt.Errorf("commit product content snapshot: %w", err)
	}
	return snapshot, nil
}
