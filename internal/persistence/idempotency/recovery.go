package idempotency

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/idempotency"
)

func BindRecovery(executor dbapi.Tx) application.RecoveryWriter {
	return recoveryWriter{executor}
}

type recoveryWriter struct{ executor dbapi.Tx }

func (writer recoveryWriter) SaveCompleted(ctx context.Context, frozen application.FrozenCommand) error {
	if frozen.ExpiresAtMS <= writer.executor.NowMS() {
		return nil
	}
	request, receipt := frozen.Request, frozen.Receipt
	if err := save(ctx, writer.executor, request.OperationID, request.Key, request.PrincipalID,
		receipt, frozen.CreatedAtMS, frozen.ExpiresAtMS); err != nil {
		return fmt.Errorf("complete recovered command: %w", err)
	}
	return nil
}
