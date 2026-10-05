package idempotency

import (
	"context"
	"fmt"
	"sync"
	"time"

	"retrom/internal/telemetry"
)

type identityGate struct {
	token      chan struct{}
	references int
}

type gates struct {
	mu   sync.Mutex
	keys map[Request]*identityGate
}

func (group *gates) acquire(ctx context.Context, request Request) (func(), error) {
	request.Digest = "" // Coordination covers conflicting payloads, too.
	group.mu.Lock()
	if group.keys == nil {
		group.keys = make(map[Request]*identityGate)
	}
	gate := group.keys[request]
	if gate == nil {
		gate = &identityGate{token: make(chan struct{}, 1)}
		gate.token <- struct{}{}
		group.keys[request] = gate
	}
	gate.references++
	group.mu.Unlock()
	drop := func() {
		group.mu.Lock()
		defer group.mu.Unlock()
		gate.references--
		if gate.references == 0 {
			delete(group.keys, request)
		}
	}
	select {
	case <-ctx.Done():
		drop()
		return nil, fmt.Errorf("wait for command identity: %w", ctx.Err())
	case <-gate.token:
		return func() { gate.token <- struct{}{}; drop() }, nil
	}
}

func (service *Service) Coordinate(ctx context.Context, request Request, command *Command,
	work func(context.Context) error,
) error {
	started := time.Now()
	release, err := service.gates.acquire(ctx, request)
	telemetry.RecordTiming(ctx, telemetry.IdempotencyWait, time.Since(started))
	if err != nil {
		return err
	}
	defer release()
	if err := service.repository.Coordinate(ctx, request, command, work); err != nil {
		return fmt.Errorf("idempotency: coordinate command: %w", err)
	}
	return nil
}
