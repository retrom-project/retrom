package blobgc

import (
	"context"
	"errors"
	"testing"
)

type gcRepository struct{ reads int }

func (repository *gcRepository) Counts(context.Context) (int, int, error) {
	repository.reads++
	if repository.reads == 1 {
		return 5, 2, nil
	}
	return 4, 1, nil
}
func (*gcRepository) Protected(context.Context) (int, error) { return 3, nil }

type gcRelease struct {
	reconciled, ran bool
	failure         error
	runFailure      error
}

func (release *gcRelease) ReconcileGC(context.Context) error {
	release.reconciled = true
	return release.failure
}

func (release *gcRelease) RunOnce(context.Context) (bool, error) {
	release.ran = true
	return true, release.runFailure
}

func TestGCReportsOnlyPositiveChanges(t *testing.T) {
	t.Parallel()
	repository, release := &gcRepository{}, &gcRelease{}
	result, err := New(repository, release).RunOnce(t.Context())
	if err != nil || result != (Result{Protected: 3, Deleted: 1, Retained: 1}) || !release.reconciled || !release.ran {
		t.Fatalf("result=%+v error=%v release=%+v", result, err, release)
	}
}

func TestGCStopsWhenProtectionReconciliationFails(t *testing.T) {
	t.Parallel()
	failure := errors.New("protection registry unavailable")
	repository, release := &gcRepository{}, &gcRelease{failure: failure}
	_, err := New(repository, release).RunOnce(t.Context())
	if !errors.Is(err, failure) || release.ran || repository.reads != 1 {
		t.Fatalf("error=%v ran=%v reads=%d", err, release.ran, repository.reads)
	}
}

func TestGCDoesNotPublishCleanupCountsAfterWorkerFailure(t *testing.T) {
	t.Parallel()
	cause := errors.New("GC worker storage unavailable")
	repository, release := &gcRepository{}, &gcRelease{runFailure: cause}
	result, err := New(repository, release).RunOnce(t.Context())
	if !errors.Is(err, cause) || result != (Result{}) || !release.ran || repository.reads != 1 {
		t.Fatalf("worker failure reported successful cleanup: result=%+v error=%v reads=%d", result, err, repository.reads)
	}
}
