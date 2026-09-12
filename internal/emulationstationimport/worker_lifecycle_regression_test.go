package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/serversource"
	"retrom/internal/testsupport"
)

func TestESWorkerCancelledContextDoesNotScheduleFailure(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancelled", true: "parent deadline"}[deadline], func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			_, unit := startLifecycleImport(t, fixture, "", "nes")
			before := executionAuthorityState(t, fixture, unit)
			ctx, cancel := context.WithCancel(fixture.context)
			if deadline {
				cancel()
				ctx, cancel = context.WithDeadline(fixture.context, time.Unix(0, 0))
			}
			cancel()
			fixture.service.fail(ctx, unit, "INTERNAL_ERROR", true)
			if after := executionAuthorityState(t, fixture, unit); after != before {
				t.Fatalf("stopped worker changed execution: before=%s after=%s", before, after)
			}
		})
	}
}

func TestESWorkerScanCancellationInterruptsReaderWait(t *testing.T) {
	fixture := newLifecycleFixture(t)
	release := holdESSourceReaders(t)
	defer release()
	created, err := fixture.service.Create(fixture.context, CreateRequest{RootID: "games", SourceRelativePath: ""}, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	unit, found, err := fixture.service.claim(fixture.context)
	if err != nil || !found {
		t.Fatalf("claim=%v error=%v", found, err)
	}
	entered, _ := observeESScanReset(t, fixture, created.ID, nil)
	ctx, cancel := context.WithCancel(fixture.context)
	done := make(chan struct{})
	go func() { defer close(done); fixture.service.execute(ctx, unit) }()
	defer func() { cancel(); release(); <-done }()
	waitESWorkerSignal(t, entered, "scan reset")
	requestExecutionCancellation(t, fixture, created.ID)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scan user cancellation did not interrupt occupied reader slots")
	}
	summary, err := fixture.service.Get(fixture.context, created.ID)
	if err != nil || summary.State != "CANCELLED" || summary.Counts.Games != 0 {
		t.Fatalf("scan=%#v error=%v", summary, err)
	}
}

func TestESWorkerCloseJoinsRunningTransaction(t *testing.T) {
	fixture := newLifecycleFixture(t)
	created, err := fixture.service.Create(fixture.context, CreateRequest{RootID: "games", SourceRelativePath: ""}, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	entered, drained := observeESScanReset(t, fixture, created.ID, release)
	fixture.service.Start()
	waitESWorkerSignal(t, entered, "running scan transaction")
	closed := make(chan struct{})
	go func() { fixture.service.Close(); close(closed) }()
	premature := false
	select {
	case <-closed:
		premature = true
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	waitESWorkerSignal(t, closed, "joined worker close")
	if premature {
		waitESWorkerSignal(t, drained, "old worker drain cleanup")
		if err := fixture.service.database.PingContext(t.Context()); err != nil {
			t.Fatal(err)
		}
		t.Fatal("Close returned before active execution transaction was released")
	}
}

func holdESSourceReaders(t *testing.T) func() {
	t.Helper()
	releases := make([]func(), 0, 2)
	for range 2 {
		release, err := serversource.AcquireReader(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			for _, release := range releases {
				release()
			}
		})
	}
}

func observeESScanReset(t *testing.T, fixture lifecycleFixture, id string, release <-chan struct{}) (<-chan struct{}, <-chan struct{}) {
	t.Helper()
	entered := make(chan struct{})
	drained := make(chan struct{})
	var claims atomic.Int64
	var once sync.Once
	hook := func(_ context.Context, query string, args []driver.NamedValue) error {
		if strings.HasPrefix(query, "DELETE FROM emulationstation_import_gamelists") && len(args) == 1 && args[0].Value == id {
			once.Do(func() {
				close(entered)
				if release != nil {
					<-release
				}
			})
		}
		return nil
	}
	fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{BeforeExec: hook, BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
		if strings.Contains(query, "ORDER BY job.available_at_ms,job.created_at_ms,job.id LIMIT 1") && claims.Add(1) == 2 {
			close(drained)
		}
		return nil
	}})
	return entered, drained
}

func waitESWorkerSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}
