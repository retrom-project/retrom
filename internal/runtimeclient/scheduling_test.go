package runtimeclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/model"
)

type scheduleEvent struct {
	Command string `json:"command"`
	Value   int    `json:"value"`
}

func schedulingPool(t *testing.T) (*workerPool, string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := `import json, os, sys, threading, time
lock=threading.Lock()
cancelled=set()
def execute(r):
    value=r['input']
    with lock:
        with open(value['events'], 'a') as f:
            f.write(json.dumps({'command':r['command'],'value':value['value']})+'\n')
    if value.get('crash'): os._exit(1)
    while not os.path.exists(value['release']):
        time.sleep(0.002)
    with lock:
        print(json.dumps({'id':r['id'],'result':{'pid':os.getpid(),'value':value['value']}}),flush=True)
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
	release := filepath.Join(root, "release")
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o600) })
	return pool, filepath.Join(root, "events"), release
}

func scheduleInput(events, release string, value int) map[string]any {
	return map[string]any{"events": events, "release": release, "value": value}
}

func readScheduleEvents(t *testing.T, path string) []scheduleEvent {
	t.Helper()
	bytes, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var result []scheduleEvent
	for _, line := range strings.Split(strings.TrimSpace(string(bytes)), "\n") {
		if line == "" {
			continue
		}
		var event scheduleEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// The fixture can be in the middle of appending its final line.
			continue
		}
		result = append(result, event)
	}
	return result
}

func waitScheduleEvents(t *testing.T, path string, count int) []scheduleEvent {
	t.Helper()
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		if events := readScheduleEvents(t, path); len(events) >= count {
			return events
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("runtime executions never reached %d: %+v", count, readScheduleEvents(t, path))
	return nil
}

func TestSharedCLINormalizationHasIndependentExecutionAndAdmission(t *testing.T) {
	pool, events, release := schedulingPool(t)
	completed := make(chan error, 11)
	for i := range 7 {
		go func() {
			completed <- pool.Call(t.Context(), "normalize-content", scheduleInput(events, release, i), &workerResult{})
		}()
	}
	select {
	case err := <-completed:
		if !errors.Is(err, model.ErrUnavailable) || !strings.Contains(err.Error(), "RUNTIME_BUSY") {
			t.Fatalf("full normalization lane must reject immediately with BUSY: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("normalization accepted more than two executions plus four queued calls")
	}
	for i := range 4 {
		go func() {
			completed <- pool.Call(context.Background(), "prepare", scheduleInput(events, release, i), &workerResult{})
		}()
	}
	started := waitScheduleEvents(t, events, 6)
	counts := make(map[string]int)
	for _, event := range started {
		counts[event.Command]++
	}
	if counts["normalize-content"] != 2 || counts["prepare"] != 4 {
		t.Fatalf("normalization occupied interactive execution slots: %+v", started)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if err := <-completed; err != nil {
			t.Fatalf("accepted request failed: %v", err)
		}
	}
}

func TestSharedCLIRunningCancellationRetainsNormalizationSlotsUntilResponse(t *testing.T) {
	pool, events, release := schedulingPool(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancelled := make(chan error, 2)
	for i := range 2 {
		go func() {
			cancelled <- pool.Call(ctx, "normalize-content", scheduleInput(events, release, i), &workerResult{})
		}()
	}
	waitScheduleEvents(t, events, 2)
	cancel()
	for range 2 {
		if err := <-cancelled; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled caller=%v", err)
		}
	}
	nextRelease := filepath.Join(filepath.Dir(release), "next-release")
	t.Cleanup(func() { _ = os.WriteFile(nextRelease, nil, 0o600) })
	completed := make(chan error, 6)
	for i := range 6 {
		go func() {
			completed <- pool.Call(t.Context(), "normalize-content", scheduleInput(events, nextRelease, i+10), &workerResult{})
		}()
	}
	for range 2 {
		select {
		case err := <-completed:
			if !errors.Is(err, model.ErrUnavailable) || !strings.Contains(err.Error(), "RUNTIME_BUSY") {
				t.Fatalf("cancelled execution released admission before response: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("cancelled execution did not retain its normalization quota")
		}
	}
	if got := readScheduleEvents(t, events); len(got) != 2 {
		t.Fatalf("replacement started before previous cancellation completed: %+v", got)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitScheduleEvents(t, events, 4)
	if err := os.WriteFile(nextRelease, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if err := <-completed; err != nil {
			t.Fatal(err)
		}
	}
}

func TestSharedCLIQueuedNormalizationDeadlineNeverExecutesAndReturnsCapacity(t *testing.T) {
	pool, events, release := schedulingPool(t)
	completed := make(chan error, 2)
	for i := range 2 {
		go func() {
			completed <- pool.Call(t.Context(), "normalize-content", scheduleInput(events, release, i), &workerResult{})
		}()
	}
	waitScheduleEvents(t, events, 2)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := pool.Call(ctx, "normalize-content", scheduleInput(events, release, 99), &workerResult{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued normalization ignored caller deadline: %v", err)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-completed; err != nil {
			t.Fatal(err)
		}
	}
	var after workerResult
	if err := pool.Call(t.Context(), "normalize-content", scheduleInput(events, release, 100), &after); err != nil {
		t.Fatal(err)
	}
	for _, event := range readScheduleEvents(t, events) {
		if event.Value == 99 {
			t.Fatal("expired queued request reached the runtime process")
		}
	}
}

func TestSharedCLICloseReleasesRunningAndQueuedNormalization(t *testing.T) {
	pool, events, release := schedulingPool(t)
	completed := make(chan error, 6)
	for i := range 6 {
		go func() {
			completed <- pool.Call(t.Context(), "normalize-content", scheduleInput(events, release, i), &workerResult{})
		}()
	}
	waitScheduleEvents(t, events, 2)
	pool.Close()
	for range 6 {
		select {
		case err := <-completed:
			if !errors.Is(err, model.ErrUnavailable) {
				t.Fatalf("closed normalization=%v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Close leaked a normalization request")
		}
	}
}

func TestSharedCLITransportFailureDispatchesOnlyUnstartedNormalizationToFreshProcess(t *testing.T) {
	pool, events, release := schedulingPool(t)
	completed := make(chan error, 6)
	for i := range 6 {
		go func() {
			completed <- pool.Call(t.Context(), "normalize-content", scheduleInput(events, release, i), &workerResult{})
		}()
	}
	waitScheduleEvents(t, events, 2)
	input := scheduleInput(events, release, 100)
	input["crash"] = true
	if err := pool.Call(t.Context(), "prepare", input, &workerResult{}); err == nil {
		t.Fatal("dead runtime reported success")
	}
	waitScheduleEvents(t, events, 5) // two old normalizations, crash, two newly dispatched.
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	failed := 0
	for range 6 {
		select {
		case err := <-completed:
			if err != nil {
				failed++
			}
		case <-time.After(2 * time.Second):
			t.Fatal("transport failure left the independent queue blocked")
		}
	}
	if failed != 2 {
		t.Fatalf("replayed or lost accepted executions: failures=%d", failed)
	}
}

func TestSharedCLIInputByteBudgetsAreIndependentAndRetainedThroughCancellation(t *testing.T) {
	for _, command := range []string{"normalize-content", "prepare"} {
		t.Run(command, func(t *testing.T) { checkInputByteBudget(t, command) })
	}
}

func checkInputByteBudget(t *testing.T, command string) {
	t.Helper()

	pool, events, release := schedulingPool(t)
	input := scheduleInput(events, release, 1)
	input["padding"] = strings.Repeat("x", 36*1024*1024)
	ctx, cancel := context.WithCancel(t.Context())
	completed := make(chan error, 1)
	go func() { completed <- pool.Call(ctx, command, input, &workerResult{}) }()
	waitScheduleEvents(t, events, 1)
	busy := func() {
		t.Helper()
		bounded, stop := context.WithTimeout(t.Context(), time.Second)
		defer stop()
		if err := pool.Call(bounded, command, input, &workerResult{}); !errors.Is(err, model.ErrUnavailable) || !strings.Contains(err.Error(), "RUNTIME_BUSY") {
			t.Fatalf("64 MiB accepted-input quota did not reject with BUSY: %v", err)
		}
	}
	busy()
	fastRelease := filepath.Join(filepath.Dir(release), "fast-release")
	if err := os.WriteFile(fastRelease, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	other := "normalize-content"
	if command == other {
		other = "configure"
	}
	if err := pool.Call(t.Context(), other, scheduleInput(events, fastRelease, 2), &workerResult{}); err != nil {
		t.Fatalf("one input quota blocked the other lane: %v", err)
	}
	cancel()
	if err := <-completed; !errors.Is(err, context.Canceled) {
		t.Fatalf("running cancellation=%v", err)
	}
	busy()
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(3 * time.Second)
	for {
		err := pool.Call(t.Context(), command, input, &workerResult{})
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "RUNTIME_BUSY") || time.Now().After(until) {
			t.Fatalf("completed execution did not release input bytes: %v", err)
		}
	}
}
