package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	payloadreleasemodel "retrom/internal/model/payloadrelease"
	persistence "retrom/internal/repo/payloadrelease"
)

type GameImpact = payloadreleasemodel.GameImpact

func GameDeleteAuditImpact(impact GameImpact) map[string]any {
	return payloadreleasemodel.GameDeleteAuditImpact(impact)
}

func GameDeleteImpact(ctx context.Context, database *sql.DB, gameID string) (GameImpact, error) {
	result, err := payloadreleasemodel.NewImpactQueries(persistence.NewImpactQueries(database)).Game(ctx, gameID)
	if err != nil {
		return GameImpact{}, fmt.Errorf("read game deletion impact: %w", err)
	}
	return result, nil
}

func GameDeleteImpactTx(ctx context.Context, transaction *sql.Tx, gameID string) (GameImpact, error) {
	result, err := payloadreleasemodel.NewImpactQueries(persistence.BindImpact(transaction)).Game(ctx, gameID)
	if err != nil {
		return GameImpact{}, fmt.Errorf("read game deletion impact in transaction: %w", err)
	}
	return result, nil
}
