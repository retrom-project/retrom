package serverimport_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	importpersistence "retrom/internal/persistence/serverimport"
	importservice "retrom/internal/service/serverimport"
)

type simultaneousCreation struct {
	importservice.CreationRepository
	reads    atomic.Int64
	arrivals chan struct{}
	release  chan struct{}
}

func (repository *simultaneousCreation) WithCreate(ctx context.Context, work func(importservice.CreationWriter) error) error {
	return repository.CreationRepository.WithCreate(ctx, func(writer importservice.CreationWriter) error {
		return work(simultaneousCreationWriter{CreationWriter: writer, repository: repository})
	})
}

type simultaneousCreationWriter struct {
	importservice.CreationWriter
	repository *simultaneousCreation
}

func (writer simultaneousCreationWriter) Active(ctx context.Context, kind string) (bool, error) {
	active, err := writer.CreationWriter.Active(ctx, kind)
	if err != nil || writer.repository.reads.Add(1) > 2 {
		return active, err
	}
	writer.repository.arrivals <- struct{}{}
	select {
	case <-writer.repository.release:
		return active, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func TestConcurrentCreationRechecksActiveImportAfterSerialization(t *testing.T) {
	legacy, database, _ := archiveImportFixture(t)
	database.SetMaxOpenConns(4)
	repository := &simultaneousCreation{
		CreationRepository: importpersistence.NewCreation(database),
		arrivals:           make(chan struct{}, 2), release: make(chan struct{}),
	}
	creation := importservice.NewCreation(repository, legacy.SourceSelectorForTest(), legacy.NowForTest)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := creation.Create(ctx, CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
			results <- err
		}()
	}
	for range 2 {
		select {
		case <-repository.arrivals:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	close(repository.release)
	successes, conflicts := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrActive):
			conflicts++
		default:
			t.Fatalf("concurrent creation returned database error: %v", err)
		}
	}
	var imports, jobs int
	err := dbapi.QueryRowContext(ctx, database, `SELECT (SELECT count(*) FROM server_imports),
(SELECT count(*) FROM jobs WHERE kind='SERVER_BIOS_IMPORT')`).Scan(&imports, &jobs)
	if err != nil || successes != 1 || conflicts != 1 || imports != 1 || jobs != 1 {
		t.Fatalf("successes=%d conflicts=%d imports=%d jobs=%d error=%v", successes, conflicts, imports, jobs, err)
	}
}
