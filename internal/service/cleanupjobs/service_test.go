package cleanupjobs

import (
	"errors"
	"testing"
)

func TestPayloadServiceCloseBeforeStartPreventsWork(t *testing.T) {
	t.Parallel()
	service, err := New(t.Context(), Dependencies{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	service.Close()
	service.Start()
	did, err := service.RunOnce(t.Context())
	if did || !errors.Is(err, ErrWorkerClosed) {
		t.Fatalf("closed facade accepted work: %t %v", did, err)
	}
}
