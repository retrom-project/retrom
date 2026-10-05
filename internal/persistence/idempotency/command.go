package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

	"retrom/internal/telemetry"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/idempotency"
)

type commandLifecycle struct {
	command *application.Command
}

func (participant commandLifecycle) Begin(ctx context.Context, tx dbapi.Tx) func(bool) {
	if !application.Active(ctx) {
		return func(bool) {}
	}
	participant.command.Begin(commandWriter{tx})
	return participant.command.End
}

type commandWriter struct{ executor dbapi.Executor }

func (writer commandWriter) EncodingContext(ctx context.Context) context.Context {
	return dbapi.WithReadScope(ctx, writer.executor)
}

func (writer commandWriter) SaveCommand(ctx context.Context, request application.Request,
	receipt application.Receipt, created, expires int64,
) error {
	return save(ctx, writer.executor, request.OperationID, request.Key, request.PrincipalID, receipt, created, expires)
}

func (repository *Repository) Coordinate(ctx context.Context, request application.Request,
	command *application.Command, work func(context.Context) error,
) error {
	identity := "retrom-http-command-v1\x00" + request.PrincipalID + "\x00" + request.OperationID + "\x00" + request.Key
	digest := sha256.Sum256([]byte(identity))
	key := int64(binary.BigEndian.Uint64(digest[:8]))
	ctx = dbapi.WithTransactionLifecycle(ctx, commandLifecycle{command})
	command.SetReadRecorder(func(ctx context.Context, result any) error {
		return dbapi.RetryTransaction(ctx, repository.database, func(dbapi.Tx) error {
			return application.Complete(ctx, result)
		})
	})
	started := time.Now()
	if err := repository.database.WithAdvisoryLock(ctx, key, func() error {
		telemetry.RecordTiming(ctx, telemetry.IdempotencyWait, time.Since(started))
		return work(ctx)
	}); err != nil {
		return fmt.Errorf("coordinate idempotent command: %w", err)
	}
	return nil
}
