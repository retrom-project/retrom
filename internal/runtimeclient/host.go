package runtimeclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/retrom-project/retrom-runtime/host"

	"retrom/internal/model"
)

const (
	maximumMessage = 64 * 1024 * 1024
	maximumPending = 32
)

var errOutputLimit = errors.New("runtime message exceeds limit")

// Host executes one runtime host command; *host.Service is the production implementation.
type Host interface {
	Call(ctx context.Context, command string, input []byte) (json.RawMessage, error)
}

// hostPool admits commands into two bounded FIFO lanes and runs them in process. A running command keeps its
// lane slot and input budget until it returns, even when its caller has already left.
type hostPool struct {
	context       context.Context
	cancel        context.CancelFunc
	executor      Host
	interactive   *requestLane
	normalization *requestLane
	running       sync.WaitGroup
	mutex         sync.Mutex
	sequence      uint64
	stops         map[uint64]context.CancelFunc
}

type hostResult struct {
	output json.RawMessage
	err    error
}

func newHostPool(ctx context.Context, runtime Host) *hostPool {
	lifetime, cancel := context.WithCancel(ctx)
	return &hostPool{
		context: lifetime, cancel: cancel, executor: runtime,
		interactive:   newRequestLane(interactiveExecutions, maximumPending),
		normalization: newRequestLane(normalizationExecutions, maximumNormalizationPending),
		stops:         make(map[uint64]context.CancelFunc),
	}
}

// Close cancels every command and waits until each has returned its slot.
func (p *hostPool) Close() {
	p.cancel()
	p.mutex.Lock()
	for _, stop := range p.stops {
		stop()
	}
	p.mutex.Unlock()
	p.running.Wait()
}

// track registers a running command's cancellation; commands started after Close are cancelled at once.
func (p *hostPool) track(stop context.CancelFunc) func() {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.sequence++
	id := p.sequence
	p.stops[id] = stop
	if p.context.Err() != nil {
		stop()
	}
	return func() {
		p.mutex.Lock()
		delete(p.stops, id)
		p.mutex.Unlock()
	}
}

func (p *hostPool) Call(ctx context.Context, command string, input, output any) error {
	bounded, cancel := context.WithTimeout(ctx, commandTimeout(command))
	defer cancel()
	if bounded.Err() != nil {
		return fmt.Errorf("runtime admission: %w", bounded.Err())
	}
	if p.context.Err() != nil {
		return fmt.Errorf("runtime stopped: %w", model.ErrUnavailable)
	}
	lane := p.interactive
	if command == "normalize-content" {
		lane = p.normalization
	}
	lease, err := lane.reserve()
	if err != nil {
		return err
	}
	if input == nil {
		input = map[string]any{}
	}
	encoded, err := lease.encode(bounded, p.context, input)
	if err == nil {
		err = lease.wait(bounded, p.context)
	}
	if err != nil {
		lease.release()
		if p.context.Err() != nil {
			return fmt.Errorf("runtime stopped: %w", model.ErrUnavailable)
		}
		return fmt.Errorf("runtime admission: %w", err)
	}
	return p.run(bounded, command, encoded, output, lease)
}

// run executes an admitted command; the lease is released only when the command itself returns.
func (p *hostPool) run(ctx context.Context, command string, encoded []byte, output any, lease *requestLease) error {
	execution, stop := context.WithCancel(ctx)
	detach := p.track(stop)
	completed := make(chan hostResult, 1)
	p.running.Add(1)
	go func() {
		defer p.running.Done()
		defer lease.release()
		defer detach()
		defer stop()
		result, err := p.executor.Call(execution, command, encoded)
		completed <- hostResult{output: result, err: err}
	}()
	select {
	case result := <-completed:
		if result.err != nil {
			return fmt.Errorf("runtime %s: %w", command, hostFailure(result.err))
		}
		if err := json.Unmarshal(result.output, output); err != nil {
			return fmt.Errorf("runtime result: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("runtime request cancelled: %w", ctx.Err())
	case <-p.context.Done():
		return fmt.Errorf("runtime stopped: %w", model.ErrUnavailable)
	}
}

func hostFailure(err error) error {
	var failure *host.Error
	if errors.As(err, &failure) {
		return runtimeFailure(failure.Code, failure.Details)
	}
	return fmt.Errorf("runtime host: %w (%w)", model.ErrUnavailable, err)
}

func commandTimeout(command string) time.Duration { return host.Deadline(command) }

func runtimeFailure(code string, details map[string]string) error {
	switch code {
	case "RUNTIME_PARENT_MISSING":
		name := details["parents"]
		if name == "" {
			name = details["logicalKey"]
		}
		return &model.RuntimeContentError{Code: code, Message: "Required Parent ROM is missing: " + name}
	case "RUNTIME_PARENT_INVALID":
		return &model.RuntimeContentError{
			Code: code, Message: "Select a Parent archive for this core; the entry archive cannot be replaced here",
		}
	}
	if failure := ContentFailure(code); failure != nil {
		return failure
	}
	category := model.ErrInvalid
	switch code {
	case "RUNTIME_BUSY", "RUNTIME_CANCELLED":
		category = model.ErrUnavailable
	case "CORE_UNAVAILABLE", "CONTENT_CHANGED", "FORMAT_UNREADABLE":
		category = model.ErrConflict
	}
	return fmt.Errorf("runtime rejected input: %w (%s)", category, code)
}

func (c *Client) Close() {
	if c.pool != nil {
		c.pool.Close()
	}
}
