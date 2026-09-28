package application

import "sync"

type backgroundWorker interface {
	Stop()
	Wait()
}

type namedWorker struct {
	name   string
	worker backgroundWorker
}

// Only lifecycle ownership is collected here; business services never look up dependencies in this list.
func (services *Services) workers() []namedWorker {
	return []namedWorker{
		{"DAT indexing", services.catalogs},
		{"import discards", services.ImportDiscards},
		{"source imports", services.SourceImports},
		{"server imports", services.ServerImports},
		{"bulk approvals", services.ReviewBulkApprovals},
		{"imports", services.Importer},
		{"variant validation", services.Variants},
		{"metadata", services.Metadata},
		{"uploads", services.Uploads},
	}
}

type shutdownGroup struct {
	once    sync.Once
	done    chan struct{}
	mutex   sync.Mutex
	pending []string
	workers []namedWorker
	cleanup func()
}

func newShutdownGroup(workers []namedWorker, cleanup func()) *shutdownGroup {
	names := make([]string, 0, len(workers)+1)
	for _, worker := range workers {
		names = append(names, worker.name)
	}
	names = append(names, "cleanup jobs")
	return &shutdownGroup{done: make(chan struct{}), pending: names, workers: workers, cleanup: cleanup}
}

func (group *shutdownGroup) start(before func()) {
	group.once.Do(func() {
		go func() {
			before()
			for _, worker := range group.workers {
				worker.worker.Stop()
			}
			// All producers are cancelled before joining any of them. Cleanup stays available until they finish.
			var wait sync.WaitGroup
			for _, worker := range group.workers {
				wait.Go(func() { worker.worker.Wait(); group.finished(worker.name) })
			}
			wait.Wait()
			group.cleanup()
			group.finished("cleanup jobs")
			close(group.done)
		}()
	})
}

func (group *shutdownGroup) finished(name string) {
	group.mutex.Lock()
	defer group.mutex.Unlock()
	for i, pending := range group.pending {
		if pending == name {
			group.pending = append(group.pending[:i], group.pending[i+1:]...)
			return
		}
	}
}

func (group *shutdownGroup) pendingNames() []string {
	group.mutex.Lock()
	defer group.mutex.Unlock()
	return append([]string(nil), group.pending...)
}
