package payloadrelease

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/payloadrelease"
)

type initializationFixture struct {
	readErr error
	pages   int
}

func (*initializationFixture) BlobEdges(context.Context) ([]model.BlobEdge, error) {
	edges := OwnershipRegistry()
	result := make([]model.BlobEdge, 0, len(edges))
	for _, edge := range edges {
		result = append(result, model.BlobEdge{Table: edge.Table, Column: edge.Column})
	}
	return result, nil
}

func (fixture *initializationFixture) Owners(context.Context, model.Scope, int) ([]model.LifecycleOwner, error) {
	fixture.pages++
	return nil, fixture.readErr
}

func TestPayloadServiceInitializationPreservesSnapshotFailure(t *testing.T) {
	t.Parallel()
	cause := errors.New("startup snapshot unavailable")
	fixture := &initializationFixture{readErr: cause}
	service, err := New(t.Context(), Dependencies{Lifecycle: fixture}, Options{Retention: 24 * time.Hour})
	if service != nil || !errors.Is(err, cause) || fixture.pages != 1 {
		t.Fatalf("failed initialization exposed service: %t %v pages=%d", service != nil, err, fixture.pages)
	}
}

func TestPayloadServiceCloseBeforeStartPreventsWork(t *testing.T) {
	t.Parallel()
	service, err := New(t.Context(), Dependencies{Lifecycle: &initializationFixture{}}, Options{Retention: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	service.Close()
	service.Start()
	did, err := service.RunOnce(t.Context())
	if did || !errors.Is(err, model.ErrWorkerClosed) {
		t.Fatalf("closed facade accepted work: %t %v", did, err)
	}
}
