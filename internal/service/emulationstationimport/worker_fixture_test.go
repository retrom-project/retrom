package emulationstationimport

import (
	"context"
	"sync/atomic"
	"time"
)

type workerFixture struct {
	claims, renewals, maintained, executed, acknowledged, failed atomic.Int64
	observation                                                  atomic.Pointer[LeaseState]
	observeErr, renewErr, ackErr, errorFailure                   error
	run                                                          func(context.Context, Execution)
	maintain                                                     func(context.Context) error
	settle                                                       func(context.Context) error
	reports                                                      chan error
}

func (fixture *workerFixture) Claim(context.Context) (Execution, bool, error) {
	return Execution{JobID: "job", ImportID: "plan", Kind: "SERVER_EMULATIONSTATION_SCAN", WorkerID: "owner", Attempt: 1, ExecutionNo: 1, DeadlineAtMS: time.Now().Add(time.Hour).UnixMilli()}, fixture.claims.Add(1) == 1, nil
}

func (fixture *workerFixture) Renew(context.Context, Execution) (LeaseState, error) {
	fixture.renewals.Add(1)
	return LeaseActive, fixture.renewErr
}

func (fixture *workerFixture) Observe(context.Context, Execution) (LeaseState, error) {
	return *fixture.observation.Load(), fixture.observeErr
}

func (fixture *workerFixture) Execute(ctx context.Context, unit Execution) {
	fixture.executed.Add(1)
	fixture.run(ctx, unit)
}

func (fixture *workerFixture) Maintain(ctx context.Context) error {
	fixture.maintained.Add(1)
	if fixture.maintain != nil {
		return fixture.maintain(ctx)
	}
	return nil
}

func (fixture *workerFixture) CloseCancelled(ctx context.Context, _ Execution) (bool, error) {
	fixture.acknowledged.Add(1)
	if fixture.settle != nil {
		return false, fixture.settle(ctx)
	}
	return true, fixture.ackErr
}

func (fixture *workerFixture) Fail(context.Context, Execution, ExecutionFailure) (string, error) {
	fixture.failed.Add(1)
	return "FAILED", fixture.errorFailure
}

func newWorkerFixture() (*Worker, *workerFixture) {
	fixture := &workerFixture{reports: make(chan error, 20)}
	fixture.observe(LeaseActive)
	fixture.run = func(ctx context.Context, _ Execution) { <-ctx.Done() }
	return NewWorker(WorkerDependencies{Leases: fixture, Maintenance: fixture, Executor: fixture, Control: fixture, Report: func(err error) { fixture.reports <- err }}, time.Now), fixture
}

func (fixture *workerFixture) observe(state LeaseState) { fixture.observation.Store(&state) }
