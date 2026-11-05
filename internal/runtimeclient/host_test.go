package runtimeclient

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/retrom-project/retrom-runtime/host"

	"retrom/internal/model"
)

type gatedValue struct {
	Value int `json:"value"`
}

// gatedHost starts each command, reports it on started and returns only when its value is released.
type gatedHost struct {
	started      chan int
	mutex        sync.Mutex
	gates        map[int]chan struct{}
	honorCancel  bool
	failure      error
	returnedOnce map[int]chan struct{}
}

func newGatedHost() *gatedHost {
	return &gatedHost{started: make(chan int, 128), gates: map[int]chan struct{}{}, returnedOnce: map[int]chan struct{}{}}
}

func (h *gatedHost) gate(value int) chan struct{} {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if h.gates[value] == nil {
		h.gates[value] = make(chan struct{})
		h.returnedOnce[value] = make(chan struct{})
	}
	return h.gates[value]
}

func (h *gatedHost) release(value int) { close(h.gate(value)) }

func (h *gatedHost) returned(value int) chan struct{} {
	h.gate(value)
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return h.returnedOnce[value]
}

func (h *gatedHost) Call(ctx context.Context, _ string, input []byte) (json.RawMessage, error) {
	var value gatedValue
	if err := json.Unmarshal(input, &value); err != nil {
		return nil, &host.Error{Code: "RUNTIME_REQUEST_INVALID"}
	}
	gate, done := h.gate(value.Value), h.returned(value.Value)
	defer close(done)
	h.started <- value.Value
	if h.honorCancel {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, &host.Error{Code: "RUNTIME_CANCELLED"}
		}
	} else {
		<-gate
	}
	if h.failure != nil {
		return nil, h.failure
	}
	return json.RawMessage(`{"value":` + strings.TrimSpace(string(rune('0'+value.Value%10))) + `}`), nil
}

func expectStarted(t *testing.T, fake *gatedHost, count int) []int {
	t.Helper()
	values := make([]int, 0, count)
	for range count {
		select {
		case value := <-fake.started:
			values = append(values, value)
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d commands started", len(values), count)
		}
	}
	return values
}

func expectNoStart(t *testing.T, fake *gatedHost) {
	t.Helper()
	select {
	case value := <-fake.started:
		t.Fatalf("command %d started beyond its lane", value)
	case <-time.After(30 * time.Millisecond):
	}
}

func callAsync(ctx context.Context, pool *hostPool, command string, value int) chan error {
	result := make(chan error, 1)
	go func() { result <- pool.Call(ctx, command, gatedValue{Value: value}, &gatedValue{}) }()
	return result
}

func TestHostPoolAdmissionBusyDoesNotDisruptAcceptedCalls(t *testing.T) {
	fake := newGatedHost()
	pool := newHostPool(t.Context(), fake)
	t.Cleanup(pool.Close)
	results := make([]chan error, 0, maximumPending)
	for value := range maximumPending {
		results = append(results, callAsync(t.Context(), pool, "prepare", value))
	}
	expectStarted(t, fake, interactiveExecutions)
	expectNoStart(t, fake)
	time.Sleep(20 * time.Millisecond)
	err := pool.Call(t.Context(), "prepare", gatedValue{Value: 99}, &gatedValue{})
	if !errors.Is(err, model.ErrUnavailable) || !strings.Contains(err.Error(), "RUNTIME_BUSY") {
		t.Fatalf("over-capacity call: %v", err)
	}
	for value := range maximumPending {
		fake.release(value)
	}
	for _, result := range results {
		if err := <-result; err != nil {
			t.Fatalf("accepted call failed: %v", err)
		}
	}
}

func TestHostPoolMapsDomainFailuresWithoutAffectingOtherCalls(t *testing.T) {
	fake := newGatedHost()
	fake.failure = &host.Error{Code: "CONTENT_CHANGED"}
	pool := newHostPool(t.Context(), fake)
	t.Cleanup(pool.Close)
	fake.release(1)
	if err := pool.Call(t.Context(), "prepare", gatedValue{Value: 1}, &gatedValue{}); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("domain failure lost its category: %v", err)
	}
	fake.failure = errors.New("unexpected")
	fake.release(2)
	if err := pool.Call(t.Context(), "prepare", gatedValue{Value: 2}, &gatedValue{}); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("untyped failure must be unavailable: %v", err)
	}
	fake.failure = nil
	fake.release(3)
	if err := pool.Call(t.Context(), "prepare", gatedValue{Value: 3}, &gatedValue{}); err != nil {
		t.Fatalf("later call failed: %v", err)
	}
}

func TestHostPoolNormalizationHasIndependentExecutionAndAdmission(t *testing.T) {
	fake := newGatedHost()
	pool := newHostPool(t.Context(), fake)
	t.Cleanup(pool.Close)
	results := make([]chan error, 0, maximumNormalizationPending)
	for value := range maximumNormalizationPending {
		results = append(results, callAsync(t.Context(), pool, "normalize-content", value))
	}
	expectStarted(t, fake, normalizationExecutions)
	expectNoStart(t, fake)
	time.Sleep(20 * time.Millisecond)
	if err := pool.Call(t.Context(), "normalize-content", gatedValue{Value: 50}, &gatedValue{}); !errors.Is(err,
		model.ErrUnavailable) {
		t.Fatalf("normalization admission is unbounded: %v", err)
	}
	fake.release(60)
	interactive := callAsync(t.Context(), pool, "prepare", 60)
	if started := expectStarted(t, fake, 1); started[0] != 60 {
		t.Fatalf("interactive command waited for normalization: %v", started)
	}
	if err := <-interactive; err != nil {
		t.Fatal(err)
	}
	for value := range maximumNormalizationPending {
		fake.release(value)
	}
	for _, result := range results {
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	}
}

func TestHostPoolRunningCancellationRetainsSlotUntilReturn(t *testing.T) {
	fake := newGatedHost()
	pool := newHostPool(t.Context(), fake)
	t.Cleanup(pool.Close)
	ctx, cancel := context.WithCancel(t.Context())
	cancelled := callAsync(ctx, pool, "normalize-content", 0)
	other := callAsync(t.Context(), pool, "normalize-content", 1)
	expectStarted(t, fake, normalizationExecutions)
	queued := callAsync(t.Context(), pool, "normalize-content", 2)
	cancel()
	if err := <-cancelled; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller: %v", err)
	}
	expectNoStart(t, fake)
	fake.release(0)
	<-fake.returned(0)
	if started := expectStarted(t, fake, 1); started[0] != 2 {
		t.Fatalf("queued command did not take the returned slot: %v", started)
	}
	fake.release(1)
	fake.release(2)
	if err := <-other; err != nil {
		t.Fatal(err)
	}
	if err := <-queued; err != nil {
		t.Fatal(err)
	}
}

func TestHostPoolQueuedDeadlineNeverExecutesAndReturnsCapacity(t *testing.T) {
	fake := newGatedHost()
	pool := newHostPool(t.Context(), fake)
	t.Cleanup(pool.Close)
	first, second := callAsync(t.Context(), pool, "normalize-content", 0), callAsync(t.Context(), pool,
		"normalize-content", 1)
	expectStarted(t, fake, normalizationExecutions)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := pool.Call(ctx, "normalize-content", gatedValue{Value: 2}, &gatedValue{}); !errors.Is(err,
		context.DeadlineExceeded) {
		t.Fatalf("queued deadline: %v", err)
	}
	fake.release(0)
	fake.release(1)
	for _, result := range []chan error{first, second} {
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	}
	fake.release(3)
	if err := pool.Call(t.Context(), "normalize-content", gatedValue{Value: 3}, &gatedValue{}); err != nil {
		t.Fatalf("capacity was not returned: %v", err)
	}
	if started := expectStarted(t, fake, 1); started[0] != 3 {
		t.Fatalf("expired queued command executed: %v", started)
	}
}

func TestHostPoolInputByteBudgetsAreIndependentPerLane(t *testing.T) {
	fake := newGatedHost()
	pool := newHostPool(t.Context(), fake)
	t.Cleanup(pool.Close)
	large := map[string]any{"value": 0, "padding": strings.Repeat("x", maximumMessage/2+1024)}
	held := make(chan error, 1)
	go func() { held <- pool.Call(t.Context(), "normalize-content", large, &gatedValue{}) }()
	expectStarted(t, fake, 1)
	again := map[string]any{"value": 1, "padding": large["padding"]}
	if err := pool.Call(t.Context(), "normalize-content", again, &gatedValue{}); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("lane byte budget exceeded: %v", err)
	}
	interactive := map[string]any{"value": 2, "padding": large["padding"]}
	fake.release(2)
	if err := pool.Call(t.Context(), "prepare", interactive, &gatedValue{}); err != nil {
		t.Fatalf("interactive budget is shared with normalization: %v", err)
	}
	fake.release(0)
	if err := <-held; err != nil {
		t.Fatal(err)
	}
}

func TestHostPoolCloseCancelsAndWaitsForRunningCommands(t *testing.T) {
	fake := newGatedHost()
	fake.honorCancel = true
	pool := newHostPool(t.Context(), fake)
	running := callAsync(t.Context(), pool, "normalize-content", 0)
	expectStarted(t, fake, 1)
	pool.Close()
	select {
	case <-fake.returned(0):
	default:
		t.Fatal("Close returned before the running command")
	}
	if err := <-running; !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("running call after Close: %v", err)
	}
	if err := pool.Call(t.Context(), "prepare", gatedValue{Value: 1}, &gatedValue{}); !errors.Is(err,
		model.ErrUnavailable) {
		t.Fatalf("call after Close: %v", err)
	}
}
