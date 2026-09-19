//go:build integration

package libraryimport

import (
	"context"
	"sync"
	"testing"

	composition "retrom/internal/bootstrap/composition/libraryimport"
	libraryimportmodel "retrom/internal/model/libraryimport"
	application "retrom/internal/service/libraryimport"
)

type importQueueGate struct {
	libraryimportmodel.ImportExecutionQueue

	release <-chan struct{}
}

func (gate importQueueGate) Claim(ctx context.Context, id string) (libraryimportmodel.ImportWork, bool, error) {
	select {
	case <-ctx.Done():
		return libraryimportmodel.ImportWork{}, false, ctx.Err()
	case <-gate.release:
	}
	return gate.ImportExecutionQueue.Claim(ctx, id)
}

func gateImportWorker(t *testing.T, service *Service) func() {
	t.Helper()
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	bundle := composition.NewWorker(service.database, service.now, service.creationDependencies(), nil)
	bundle.Worker = application.NewImportWorker(libraryimportmodel.ImportWorkerDependencies{
		Queue:       importQueueGate{ImportExecutionQueue: bundle.Executions, release: release},
		Control:     bundle.Executions,
		Recovery:    bundle.Executions,
		Preparation: service.importPreparation(),
		Creations:   service.importCreations(),
	}, application.ImportWorkerSettings{Now: service.now},
	)
	service.worker = &bundle
	t.Cleanup(func() { service.Close(); unblock() })
	return unblock
}
