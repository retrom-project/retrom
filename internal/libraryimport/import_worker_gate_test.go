//go:build integration

package libraryimport

import (
	"context"
	"sync"
	"testing"

	composition "retrom/internal/composition/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

type importQueueGate struct {
	libraryservice.ImportExecutionQueue
	release <-chan struct{}
}

func (gate importQueueGate) Claim(ctx context.Context, id string) (libraryservice.ImportWork, bool, error) {
	select {
	case <-ctx.Done():
		return libraryservice.ImportWork{}, false, ctx.Err()
	case <-gate.release:
	}
	return gate.ImportExecutionQueue.Claim(ctx, id)
}

func gateImportWorker(t *testing.T, service *Service) func() {
	t.Helper()
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	service.worker.Close()
	service.worker = libraryservice.NewImportWorker(libraryservice.ImportWorkerDependencies{
		Queue:   importQueueGate{ImportExecutionQueue: service.executions, release: release},
		Control: service.executions, Recovery: service.executions, Preparation: service.preparation, Creations: service.creations,
	}, libraryservice.ImportWorkerSettings{Now: service.now})
	service.admissions = composition.NewImportAdmissions(service.database, service.worker, service.tags,
		libraryservice.ImportAdmissionOptions{Now: service.now, MultiDiscEnabled: service.multiDiscImportEnabled})

	service.Start()
	t.Cleanup(func() { service.Close(); unblock() })
	return unblock
}
