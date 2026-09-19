package dependencies

import (
	"context"
	"errors"
	"testing"
	"time"

	dependencyfiles "retrom/internal/adapter/runtime/dependencies"
	"retrom/internal/capability/content/firmwaremanifest"
	model "retrom/internal/model/dependencies"
)

func TestFirmwareAcquisitionFailurePrecedesWriteScope(t *testing.T) {
	failure := errors.New("source unavailable")
	source := &firmwareSourceStub{failure: failure}
	repository := &firmwareWriteStub{source: source}
	service := New(firmwareSourceSet(), repository, source)
	err := service.Bootstrap(t.Context(), time.UnixMilli(1000))
	if !errors.Is(err, failure) || err.Error() != "bootstrap dependency definitions: load source firmware catalog: source unavailable" {
		t.Fatalf("source error mapping: %v", err)
	}
	if source.calls != 1 || repository.calls != 0 {
		t.Fatalf("source calls=%d write scopes=%d", source.calls, repository.calls)
	}
}

func TestFirmwareAcquiredOnceBeforeWriteScope(t *testing.T) {
	failure := errors.New("writer unavailable")
	source := &firmwareSourceStub{}
	repository := &firmwareWriteStub{source: source, failure: failure}
	service := New(firmwareSourceSet(), repository, source)
	err := service.Bootstrap(t.Context(), time.UnixMilli(1000))
	if !errors.Is(err, failure) || source.calls != 1 || repository.calls != 1 || repository.sourceCallsAtWrite != 1 {
		t.Fatalf("acquisition order: source=%d writes=%d sourceAtWrite=%d err=%v",
			source.calls, repository.calls, repository.sourceCallsAtWrite, err)
	}
}

func TestNoDependencyVersionsDoNotAcquireFirmware(t *testing.T) {
	source := &firmwareSourceStub{failure: errors.New("must not acquire")}
	repository := &firmwareWriteStub{source: source}
	service := New(&dependencyfiles.Set{}, repository, source)
	if err := service.Bootstrap(t.Context(), time.UnixMilli(1000)); err != nil {
		t.Fatal(err)
	}
	if source.calls != 0 || repository.calls != 1 {
		t.Fatalf("empty version compatibility: source=%d writes=%d", source.calls, repository.calls)
	}
}

func firmwareSourceSet() *dependencyfiles.Set {
	return &dependencyfiles.Set{
		Order:    []string{"v"},
		Versions: map[string]*dependencyfiles.Version{"v": {}},
	}
}

type firmwareSourceStub struct {
	calls   int
	failure error
}

func (source *firmwareSourceStub) LoadCatalog() (firmwaremanifest.Catalog, error) {
	source.calls++
	return firmwaremanifest.Catalog{}, source.failure
}

type firmwareWriteStub struct {
	model.Repository
	source                    *firmwareSourceStub
	calls, sourceCallsAtWrite int
	failure                   error
}

func (repository *firmwareWriteStub) WithWrite(context.Context, func(model.WriteScope) error) error {
	repository.calls++
	repository.sourceCallsAtWrite = repository.source.calls
	return repository.failure
}
