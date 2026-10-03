package serverimport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoveryOfNonCandidatesDoesNotWriteHeartbeats(t *testing.T) {
	root := t.TempDir()
	for index := range 500 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("other-%d.txt", index)), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := directory.Close(); err != nil {
			t.Error(err)
		}
	})
	lease := time.Now().Add(time.Minute).UnixMilli()
	unit := work{ImportID: "import", JobID: "job", Execution: 1, Owner: "worker"}
	memory := &leaseMemory{snapshot: LeaseSnapshot{Work: unit, State: "RUNNING", ImportState: "RUNNING", LeaseUntil: &lease}}
	service := &Service{scanLimits: defaultScanLimits(), leases: NewLeases(memory, time.Now)}
	groups, counts, err := service.discoverCandidates(t.Context(), unit, directory, nil)
	if err != nil || len(groups) != 0 || counts.Files != 500 || memory.writes != 0 {
		t.Fatalf("non-candidate scan: groups=%d counts=%+v writes=%d err=%v", len(groups), counts, memory.writes, err)
	}
}

func TestCancellationMonitorStopsLocalWorkWithoutRenewingLease(t *testing.T) {
	unit := work{ImportID: "import", JobID: "job", Execution: 1, Owner: "worker"}
	memory := &leaseMemory{snapshot: LeaseSnapshot{Work: unit, State: "CANCEL_REQUESTED"}}
	service := &Service{leases: NewLeases(memory, time.Now), stop: make(chan struct{})}
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	done := make(chan struct{})
	go func() { service.monitorExecution(ctx, unit, done, cancel); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * cancellationPollInterval):
		t.Fatal("cancellation was not observed within its bounded poll interval")
	}
	if !errors.Is(context.Cause(ctx), ErrWorkerCancelled) || memory.writes != 0 {
		t.Fatalf("cause=%v writes=%d", context.Cause(ctx), memory.writes)
	}
}

func TestProgressWritesAreBoundedButPhaseAndCompletionAreImmediate(t *testing.T) {
	progress := &progressThrottle{}
	now := time.Unix(1, 0)
	if !progress.allow("INSTALLING", 0, 10000, now) {
		t.Fatal("initial progress missing")
	}
	for index := int64(1); index < 10000; index++ {
		if progress.allow("INSTALLING", index, 10000, now) {
			t.Fatal("progress writes grew with item count")
		}
	}
	if !progress.allow("INSTALLING", 9999, 10000, now.Add(time.Second)) || !progress.allow("INSTALLING", 10000, 10000, now.Add(time.Second)) || !progress.allow("FINISHING", 0, 1, now.Add(time.Second)) {
		t.Fatal("timed, final or phase progress was dropped")
	}
}
