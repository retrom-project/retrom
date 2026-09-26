package payloadrelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"

	persistence "retrom/internal/persistence/payloadrelease"
	application "retrom/internal/service/payloadrelease"
)

type GameImpact = application.GameImpact

func GameDeleteAuditImpact(impact GameImpact) map[string]any {
	return application.GameDeleteAuditImpact(impact)
}

func GameDeleteImpact(ctx context.Context, database dbapi.DB, gameID string) (GameImpact, error) {
	result, err := application.NewImpactQueries(persistence.NewImpactQueries(database)).Game(ctx, gameID)
	if err != nil {
		return GameImpact{}, fmt.Errorf("read game deletion impact: %w", err)
	}
	return result, nil
}

func GameDeleteImpactTx(ctx context.Context, transaction dbapi.Tx, gameID string) (GameImpact, error) {
	result, err := application.NewImpactQueries(persistence.BindImpact(transaction)).Game(ctx, gameID)
	if err != nil {
		return GameImpact{}, fmt.Errorf("read game deletion impact in transaction: %w", err)
	}
	return result, nil
}
