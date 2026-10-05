package idempotency

import (
	"context"
	"fmt"
	"time"
)

// FrozenCommand travels with a recoverable domain intent. It is completed in
// the transaction publishing the final business records, including recovery.
type FrozenCommand struct {
	Request     Request
	Receipt     Receipt
	CreatedAtMS int64
	ExpiresAtMS int64
}

type RecoveryWriter interface {
	SaveCompleted(context.Context, FrozenCommand) error
}

func Freeze(ctx context.Context, result any) (*FrozenCommand, error) {
	command, _ := ctx.Value(commandKey{}).(*Command)
	if command == nil {
		return nil, ErrCommandIncomplete
	}
	if command.writer == nil || command.encode == nil {
		return nil, ErrCommandIncomplete
	}
	receipt, err := command.encode(command.writer.EncodingContext(ctx), result)
	if err != nil {
		return nil, fmt.Errorf("freeze recoverable command: %w", err)
	}
	if !validReceipt(receipt) {
		return nil, ErrInvalidReceipt
	}
	receipt.RequestDigest = command.request.Digest
	return &FrozenCommand{
		Request: command.request, Receipt: receipt, CreatedAtMS: command.now,
		ExpiresAtMS: command.now + (24 * time.Hour).Milliseconds(),
	}, nil
}

func SameRequest(ctx context.Context, request Request) bool {
	current := RequestFromContext(ctx)
	return current != nil && *current == request
}
