package runtimeclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/model"
)

func TestWorkerReusesProcessRejectsDomainErrorAndReplacesCancelledProcess(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := `import json, os, sys, time
for line in sys.stdin:
    r=json.loads(line)
    assert isinstance(r['input'], dict)
    if r['command']=='hang': time.sleep(30)
    if r['command']=='invalid': v={'id':r['id'],'error':'INVALID_CONFIGURATION'}
    elif r['command']=='conflict': v={'id':r['id'],'error':'CORE_CHANGED'}
    else: v={'id':r['id'],'result':{'pid':os.getpid()}}
    print(json.dumps(v), flush=True)
`
	if err := os.WriteFile(filepath.Join(root, "scripts/runtime-cli.mjs"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	pool := newWorkerPool(t.Context(), root, "python3")
	defer pool.Close()
	worker := <-pool.available
	type result struct {
		PID int `json:"pid"`
	}
	var first, next result
	if err := worker.call(t.Context(), pool.context, "valid", nil, &first); err != nil {
		t.Fatal(err)
	}
	if err := worker.call(t.Context(), pool.context, "invalid", nil, &next); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("domain error=%v", err)
	}
	if err := worker.call(t.Context(), pool.context, "conflict", nil, &next); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("restore conflict=%v", err)
	}
	if err := worker.call(t.Context(), pool.context, "valid", nil, &next); err != nil {
		t.Fatal(err)
	}
	if first.PID != next.PID {
		t.Fatalf("worker was not reused: %d -> %d", first.PID, next.PID)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := worker.call(ctx, pool.context, "hang", nil, &next); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel error=%v", err)
	}
	if err := worker.call(t.Context(), pool.context, "valid", nil, &next); err != nil {
		t.Fatal(err)
	}
	if first.PID == next.PID {
		t.Fatal("cancelled worker was reused")
	}
	pool.available <- worker
}
