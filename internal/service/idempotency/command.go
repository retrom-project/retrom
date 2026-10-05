package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"retrom/internal/telemetry"
)

var ErrCommandIncomplete = errors.New("IDEMPOTENCY_COMMAND_INCOMPLETE")

// Request freezes the authenticated command identity and semantic input digest.
// It is also safe to persist in a recoverable filesystem publication intent.
type Request struct {
	PrincipalID string
	OperationID string
	Key         string
	Digest      string
}

type Encoder func(context.Context, any) (Receipt, error)

type CommandWriter interface {
	EncodingContext(context.Context) context.Context
	SaveCommand(context.Context, Request, Receipt, int64, int64) error
}

// Result carries domain output and resource metadata; HTTP owns status and URL mapping.
type Result struct {
	Value      any
	Version    int64
	ResourceID string
	Accepted   bool
	Created    bool
}

type commandKey struct{}

// Command is owned by a single synchronous application invocation. A domain
// explicitly completes it inside its final write scope; intermediate work does
// not accidentally create a successful receipt.
type Command struct {
	request    Request
	encode     Encoder
	recordRead func(context.Context, any) error
	now        int64
	writer     CommandWriter
	pending    *Receipt
	committed  *Receipt
	failure    error
}

func NewCommand(ctx context.Context, request Request, encode Encoder, now int64) (context.Context, *Command) {
	command := &Command{request: request, encode: encode, now: now}
	return context.WithValue(ctx, commandKey{}, command), command
}

func RequestFromContext(ctx context.Context) *Request {
	command, _ := ctx.Value(commandKey{}).(*Command)
	if command == nil {
		return nil
	}
	request := command.request
	return &request
}

func Active(ctx context.Context) bool { return RequestFromContext(ctx) != nil }

// WithoutCommand prevents workers from sharing an HTTP invocation's mutable
// transaction participant while preserving the principal and tracing context.
func WithoutCommand(ctx context.Context) context.Context {
	return context.WithValue(ctx, commandKey{}, (*Command)(nil))
}

func (command *Command) Begin(writer CommandWriter) {
	command.writer, command.pending, command.failure = writer, nil, nil
}

func (command *Command) End(committed bool) {
	if committed && command.pending != nil {
		command.committed = command.pending
	}
	command.writer, command.pending = nil, nil
}

func (command *Command) Receipt() (Receipt, bool) {
	if command.committed == nil {
		return Receipt{}, false
	}
	return *command.committed, true
}

// Complete persists the frozen response using the executor bound to the
// business transaction. A receipt failure is returned to that transaction's
// callback, so business mutations and the receipt both roll back.
func Complete(ctx context.Context, result any) error {
	command, _ := ctx.Value(commandKey{}).(*Command)
	if command == nil {
		return nil
	}
	command.failure = command.complete(ctx, result)
	return command.failure
}

func (command *Command) complete(ctx context.Context, result any) error {
	if command.writer == nil || command.pending != nil || command.committed != nil || command.encode == nil {
		return ErrCommandIncomplete
	}
	receipt, err := command.encode(command.writer.EncodingContext(ctx), result)
	if err != nil {
		return fmt.Errorf("encode command result: %w", err)
	}
	if !validReceipt(receipt) {
		return ErrInvalidReceipt
	}
	receipt.RequestDigest = command.request.Digest
	started := time.Now()
	defer func() { telemetry.RecordTiming(ctx, telemetry.ReceiptIO, time.Since(started)) }()
	if err := command.writer.SaveCommand(ctx, command.request, receipt, command.now,
		command.now+(24*time.Hour).Milliseconds()); err != nil {
		return fmt.Errorf("persist command result: %w", err)
	}
	command.pending = &receipt
	return nil
}

// SetReadRecorder binds storage for commands that have no business mutation.
func (command *Command) SetReadRecorder(record func(context.Context, any) error) {
	command.recordRead = record
}

func CompleteRead(ctx context.Context, result any) error {
	command, _ := ctx.Value(commandKey{}).(*Command)
	if command == nil {
		return nil
	}
	if command.recordRead == nil {
		return ErrCommandIncomplete
	}
	return command.recordRead(ctx, result)
}

func (command *Command) Failure() error { return command.failure }

func validReceipt(receipt Receipt) bool {
	return receipt.HTTPStatus >= 200 && receipt.HTTPStatus < 300 && len(receipt.Body) <= 1<<20 &&
		json.Valid([]byte(receipt.HeadersJSON))
}
