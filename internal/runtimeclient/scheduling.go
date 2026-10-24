package runtimeclient

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
)

const (
	interactiveExecutions       = 4
	normalizationExecutions     = 2
	maximumNormalizationPending = 6
)

// Each lane owns its accepted input and a FIFO queue. A dispatched lease lives
// until a response or transport failure, even if the caller has already left.
type requestLane struct {
	mutex       sync.Mutex
	admission   chan struct{}
	encoding    chan struct{}
	concurrency int
	active      int
	bytes       int
	queue       []*requestLease
}

type requestLease struct {
	lane     *requestLane
	ready    chan struct{}
	size     int
	queued   bool
	running  bool
	released bool
}

func newRequestLane(concurrency, capacity int) *requestLane {
	return &requestLane{
		concurrency: concurrency, admission: make(chan struct{}, capacity), encoding: make(chan struct{}, 1),
	}
}

// Bound simultaneous encoding scratch to one frame per lane, in addition to
// the accepted-byte budget. Waiting to encode still observes both lifetimes.
func (lease *requestLease) encode(ctx, lifetime context.Context, id, command string, input any) ([]byte, error) {
	select {
	case lease.lane.encoding <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("runtime input: %w", ctx.Err())
	case <-lifetime.Done():
		return nil, fmt.Errorf("runtime lifetime: %w", lifetime.Err())
	}
	defer func() { <-lease.lane.encoding }()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("runtime input: %w", err)
	}
	if err := lifetime.Err(); err != nil {
		return nil, fmt.Errorf("runtime lifetime: %w", err)
	}
	encoded, err := json.Marshal(map[string]any{"id": id, "command": command, "input": input})
	if err != nil {
		return nil, fmt.Errorf("runtime input: %w", err)
	}
	if len(encoded) > maximumMessage {
		return nil, errOutputLimit
	}
	if err := lease.enqueue(len(encoded)); err != nil {
		return nil, err
	}
	return encoded, nil
}

func (lane *requestLane) reserve() (*requestLease, error) {
	select {
	case lane.admission <- struct{}{}:
		return &requestLease{lane: lane, ready: make(chan struct{})}, nil
	default:
		return nil, runtimeFailure("RUNTIME_BUSY", nil)
	}
}

func (lease *requestLease) enqueue(size int) error {
	lane := lease.lane
	lane.mutex.Lock()
	defer lane.mutex.Unlock()
	if size > maximumMessage-lane.bytes {
		return runtimeFailure("RUNTIME_BUSY", nil)
	}
	lease.size = size
	lane.bytes += size
	lease.queued = true
	lane.queue = append(lane.queue, lease)
	lane.dispatch()
	return nil
}

// The lane mutex must be held. Independent lanes cannot consume each other's slots.
func (lane *requestLane) dispatch() {
	for lane.active < lane.concurrency && len(lane.queue) > 0 {
		lease := lane.queue[0]
		lane.queue = slices.Delete(lane.queue, 0, 1)
		lease.queued = false
		lease.running = true
		lane.active++
		close(lease.ready)
	}
}

func (lease *requestLease) wait(ctx, lifetime context.Context) error {
	select {
	case <-lease.ready:
		if err := lifetime.Err(); err != nil {
			return fmt.Errorf("runtime lifetime: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("runtime queued request: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("runtime queued request: %w", ctx.Err())
	case <-lifetime.Done():
		return fmt.Errorf("runtime lifetime: %w", lifetime.Err())
	}
}

func (lease *requestLease) release() {
	lane := lease.lane
	lane.mutex.Lock()
	defer lane.mutex.Unlock()
	if lease.released {
		return
	}
	lease.released = true
	if lease.queued {
		index := slices.Index(lane.queue, lease)
		lane.queue = slices.Delete(lane.queue, index, index+1)
	}
	if lease.running {
		lane.active--
	}
	lane.bytes -= lease.size
	<-lane.admission
	lane.dispatch()
}
