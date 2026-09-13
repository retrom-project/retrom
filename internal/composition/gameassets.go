package composition

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"retrom/internal/blobstore"
	payloadcomposition "retrom/internal/composition/payloadrelease"
	"retrom/internal/dbexec"
	gameassetspersistence "retrom/internal/persistence/gameassets"
	gameassetsservice "retrom/internal/service/gameassets"
)

var (
	errGameAssetTransactionUnavailable = errors.New("game asset payload release requires transaction")
	errGameAssetReleaseUnavailable     = errors.New("game asset payload release service unavailable")
)

// NewGameAssets wires the game asset application service to its database and
// the caller-owned payload release scheduler.
func NewGameAssets(
	database *sql.DB,
	blobs *blobstore.Store,
	now func() time.Time,
	payloadReleases *payloadcomposition.Service,
) *gameassetsservice.Service {
	return gameassetsservice.New(
		gameassetspersistence.New(database, gameAssetPayloadReleases{service: payloadReleases}),
		blobs,
		now,
	)
}

type gameAssetPayloadReleases struct {
	service *payloadcomposition.Service
}

func (releases gameAssetPayloadReleases) transaction(
	executor dbexec.Executor,
) (*sql.Tx, error) {
	tx, ok := executor.(*sql.Tx)
	if !ok {
		return nil, errGameAssetTransactionUnavailable
	}
	if releases.service == nil {
		return nil, errGameAssetReleaseUnavailable
	}
	return tx, nil
}

func (releases gameAssetPayloadReleases) StageCandidates(
	ctx context.Context, executor dbexec.Executor, ids []string,
) error {
	tx, err := releases.transaction(executor)
	if err != nil {
		return err
	}
	if err := releases.service.StageCandidates(ctx, tx, ids); err != nil {
		return fmt.Errorf("stage game asset payload candidates: %w", err)
	}
	return nil
}

func (releases gameAssetPayloadReleases) ScheduleConsumption(
	ctx context.Context, executor dbexec.Executor, id string, now int64,
) error {
	tx, err := releases.transaction(executor)
	if err != nil {
		return err
	}
	_, err = releases.service.ScheduleConsumption(ctx, tx, id, now)
	if err != nil {
		return fmt.Errorf("schedule game asset payload consumption: %w", err)
	}
	return nil
}
