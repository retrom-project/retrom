package metadatascrape

import (
	"context"
	"errors"
	"testing"
	"time"
)

type lifecycleRunner struct {
	entered, exited chan struct{}
	cause           error
	recovered       []string
}

func (runner *lifecycleRunner) Run(ctx context.Context, _ string) error {
	close(runner.entered)
	<-ctx.Done()
	runner.cause = context.Cause(ctx)
	close(runner.exited)
	return runner.cause
}

func (runner *lifecycleRunner) Recover(context.Context) ([]string, error) {
	return runner.recovered, nil
}

func TestMetadataDispatchIsJoinedAndRejectsExecutionAfterClose(t *testing.T) {
	runner := &lifecycleRunner{entered: make(chan struct{}), exited: make(chan struct{})}
	service := New(nil, runner, func() time.Time { return time.UnixMilli(100) })
	request, cancel := context.WithCancel(t.Context())
	cancel()
	if !service.Dispatch(request, "run") {
		t.Fatal("dispatch rejected before close")
	}
	<-runner.entered
	service.Close()
	<-runner.exited
	if !errors.Is(runner.cause, ErrWorkerClosed) {
		t.Fatalf("close cause=%v", runner.cause)
	}
	if service.Dispatch(t.Context(), "run") {
		t.Fatal("dispatch after close accepted")
	}
	if err := service.Run(t.Context(), "run"); !errors.Is(err, ErrWorkerClosed) {
		t.Fatalf("Run after close=%v", err)
	}
}

type limitedRunner struct{ entered chan string }

func (runner limitedRunner) Run(ctx context.Context, id string) error {
	runner.entered <- id
	<-ctx.Done()
	return context.Cause(ctx)
}
func (limitedRunner) Recover(context.Context) ([]string, error) { return nil, nil }

func TestMetadataDispatcherLimitsConcurrentExecutions(t *testing.T) {
	runner := limitedRunner{entered: make(chan string, 2)}
	service := New(nil, runner, func() time.Time { return time.UnixMilli(100) })
	t.Cleanup(service.Close)
	if !service.Dispatch(t.Context(), "first") || !service.Dispatch(t.Context(), "second") {
		t.Fatal("available worker slot rejected")
	}
	<-runner.entered
	<-runner.entered
	if service.Dispatch(t.Context(), "third") || service.Dispatch(t.Context(), "first") {
		t.Fatal("dispatcher exceeded capacity or duplicated run")
	}
	if err := service.Run(t.Context(), "third"); !errors.Is(err, ErrExecutionBusy) {
		t.Fatalf("synchronous capacity rejection=%v", err)
	}
	service.Close()
	if err := service.Recover(t.Context()); !errors.Is(err, ErrWorkerClosed) {
		t.Fatalf("closed recovery=%v", err)
	}
}

func TestMediaDispatcherHasIndependentCapacityAndJoinsOnClose(t *testing.T) {
	scrape, media := limitedRunner{entered: make(chan string, 2)}, limitedRunner{entered: make(chan string, 2)}
	service := NewWithMedia(nil, scrape, media, func() time.Time { return time.UnixMilli(100) })
	request, cancel := context.WithCancel(t.Context())
	cancel()
	if !service.Dispatch(request, "s1") || !service.Dispatch(request, "s2") ||
		!service.ResumeMediaJob(request, "m1") || !service.ResumeMediaJob(request, "m2") {
		t.Fatal("worker families did not have independent slots")
	}
	<-scrape.entered
	<-scrape.entered
	<-media.entered
	<-media.entered
	if service.ResumeMediaJob(request, "m3") {
		t.Fatal("media capacity exceeded")
	}
	service.Close()
	if service.ResumeMediaJob(request, "m1") {
		t.Fatal("media registered after close")
	}
}

type drainingMetadataRunner struct{ entered, cancelled, release chan struct{} }

func (runner drainingMetadataRunner) Run(ctx context.Context, _ string) error {
	close(runner.entered)
	<-ctx.Done()
	close(runner.cancelled)
	<-runner.release
	return context.Cause(ctx)
}
func (drainingMetadataRunner) Recover(context.Context) ([]string, error) { return nil, nil }

func TestMediaRegistrationStopsWhileMetadataCloseIsStillJoining(t *testing.T) {
	runner := drainingMetadataRunner{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	service := NewWithMedia(nil, runner, limitedRunner{entered: make(chan string, 1)}, mediaUnitNow)
	if !service.Dispatch(t.Context(), "scrape") {
		t.Fatal("scrape rejected")
	}
	<-runner.entered
	closed := make(chan struct{})
	go func() { service.Close(); close(closed) }()
	<-runner.cancelled
	accepted := service.ResumeMediaJob(t.Context(), "late-media")
	close(runner.release)
	<-closed
	if accepted {
		t.Fatal("MEDIA registered while Service.Close was joining metadata")
	}
}
