package gamevariant

import (
	"context"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/gamevariant"
)

func ContentSnapshot(
	ctx context.Context,
	executor dbapi.Executor,
	source application.Source,
) (application.Snapshot, error) {
	game, err := Files(ctx, executor, source.GameID, false)
	if err != nil {
		return application.Snapshot{}, err
	}
	variant, err := Files(ctx, executor, source.VariantID, true)
	if err != nil {
		return application.Snapshot{}, err
	}
	return application.Snapshot{Found: true, Source: source, GameFiles: game, VariantFiles: variant}, nil
}
