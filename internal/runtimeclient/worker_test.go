package runtimeclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"retrom/internal/model"
)

type workerResult struct {
	PID   int `json:"pid"`
	Value int `json:"value"`
}

func fixturePool(t *testing.T) *workerPool {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := `import json, os, sys, threading, time
lock=threading.Lock()
cancelled=set()
def execute(r):
    time.sleep(r['input'].get('delay', 0))
    if r['command']=='crash': os._exit(1)
    if r['command']=='invalid': v={'id':r['id'],'error':'INVALID_CONFIGURATION'}
    elif r['command']=='conflict': v={'id':r['id'],'error':'CONTENT_CHANGED'}
    else: v={'id':r['id'],'result':{'pid':os.getpid(), 'value':r['input'].get('value',0)}}
    with lock:
        print(json.dumps(v), flush=True)
for line in sys.stdin:
    r=json.loads(line)
    if 'cancel' in r:
        with lock: cancelled.add(r['cancel'])
    else: threading.Thread(target=execute,args=(r,),daemon=True).start()
`
	if err := os.WriteFile(filepath.Join(root, "scripts/runtime-cli.mjs"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	pool := newWorkerPool(t.Context(), root, "python3")
	t.Cleanup(pool.Close)
	return pool
}

func TestSharedCLIConcurrentResponsesAreCorrelated(t *testing.T) {
	pool := fixturePool(t)
	const count = 12
	results := make([]workerResult, count)
	failures := make([]error, count)
	var group sync.WaitGroup
	for i := range count {
		group.Go(func() {
			failures[i] = pool.Call(t.Context(), "valid", map[string]any{"value": i, "delay": float64(count-i) * 0.005}, &results[i])
		})
	}
	group.Wait()
	for i := range count {
		if failures[i] != nil {
			t.Fatal(failures[i])
		}
		if results[i].Value != i {
			t.Fatalf("response %d routed to wrong caller: %+v", i, results[i])
		}
		if results[i].PID != results[0].PID {
			t.Fatalf("requests started separate CLIs: %d and %d", results[0].PID, results[i].PID)
		}
	}
}

func TestSharedCLIAdmissionBusyDoesNotDisruptAcceptedCalls(t *testing.T) {
	pool, events, release := schedulingPool(t)
	const count = maximumPending + 1
	completed := make(chan error, count)
	for i := range count {
		go func() {
			completed <- pool.Call(t.Context(), "prepare", scheduleInput(events, release, i), &workerResult{})
		}()
	}
	select {
	case err := <-completed:
		if !errors.Is(err, model.ErrUnavailable) || !strings.Contains(err.Error(), "RUNTIME_BUSY") {
			t.Fatalf("overflow did not reject with BUSY: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("interactive admission accepted more than 32 requests")
	}
	if started := waitScheduleEvents(t, events, 4); len(started) != 4 {
		t.Fatalf("interactive execution limit changed: %+v", started)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for range maximumPending {
		if err := <-completed; err != nil {
			t.Fatal(err)
		}
	}
}

func TestSharedCLICancellationAndDomainFailurePreserveOtherRequests(t *testing.T) {
	pool := fixturePool(t)
	var first, next workerResult
	if err := pool.Call(t.Context(), "valid", nil, &first); err != nil {
		t.Fatal(err)
	}
	if err := pool.Call(t.Context(), "invalid", nil, &next); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("domain error=%v", err)
	}
	if err := pool.Call(t.Context(), "conflict", nil, &next); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("restore conflict=%v", err)
	}
	completed := make(chan error, 1)
	go func() { completed <- pool.Call(t.Context(), "valid", map[string]any{"delay": 0.12}, &next) }()
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	var discarded workerResult
	if err := pool.Call(ctx, "valid", map[string]any{"delay": 0.2}, &discarded); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel error=%v", err)
	}
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if first.PID != next.PID {
		t.Fatalf("cancellation restarted shared CLI: %d -> %d", first.PID, next.PID)
	}
	time.Sleep(150 * time.Millisecond)
	if err := pool.Call(t.Context(), "valid", map[string]any{"value": 99}, &next); err != nil {
		t.Fatal(err)
	}
	if next.PID != first.PID || next.Value != 99 {
		t.Fatalf("late response corrupted process: %+v", next)
	}
}

func TestSharedCLIExitFailsPendingRequestsAndNextCallRecovers(t *testing.T) {
	pool := fixturePool(t)
	var first, next workerResult
	if err := pool.Call(t.Context(), "valid", nil, &first); err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	go func() { completed <- pool.Call(t.Context(), "valid", map[string]any{"delay": 0.4}, &next) }()
	if err := pool.Call(t.Context(), "crash", map[string]any{"delay": 0.08}, &workerResult{}); err == nil {
		t.Fatal("process exit succeeded")
	}
	if err := <-completed; err == nil {
		t.Fatal("pending request survived process exit")
	}
	if err := pool.Call(t.Context(), "valid", nil, &next); err != nil {
		t.Fatal(err)
	}
	if first.PID == next.PID {
		t.Fatal("dead process was reused")
	}
}

func TestSharedCLICloseUnblocksPendingRequests(t *testing.T) {
	pool := fixturePool(t)
	completed := make(chan error, 1)
	go func() { completed <- pool.Call(t.Context(), "valid", map[string]any{"delay": 5}, &workerResult{}) }()
	time.Sleep(50 * time.Millisecond)
	pool.Close()
	select {
	case err := <-completed:
		if err == nil {
			t.Fatal("closed request succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("close left a request blocked")
	}
	if err := pool.Call(t.Context(), "valid", nil, &workerResult{}); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("closed call=%v", err)
	}
}
