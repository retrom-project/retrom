package libraryimport

import (
	"reflect"
	"testing"
	"time"

	dbsqlite "retrom/internal/database/sqlite"
)

func TestImporterRequiresCompleteDependencies(t *testing.T) {
	service := newTestImporter(t, nil, nil, testImportOptions{})
	deps := assembleTestDependencies(testImportOptions{
		Database: service.database, Files: service.blobs, Tags: service.tags, Now: time.Now,
	})
	t.Cleanup(deps.Worker.Close)
	value := reflect.ValueOf(deps)
	for i := 0; i < value.NumField(); i++ {
		t.Run(value.Type().Field(i).Name, func(t *testing.T) {
			missing := deps
			reflect.ValueOf(&missing).Elem().Field(i).SetZero()
			defer func() {
				if recover() == nil {
					t.Fatal("incomplete workflow accepted")
				}
			}()
			New(missing, Options{})
		})
	}
}

func TestImporterCloseKeepsInjectedWorkerClosed(t *testing.T) {
	db, err := dbsqlite.Open(":memory:", dbsqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	service := newTestImporter(t, db, nil, testImportOptions{})
	worker, creations, preparation, approvals := service.worker, service.creations, service.preparation, service.approvals
	service.Close()
	service.Start(t.Context())
	service.NotifyImportGroup(t.Context(), "unused")
	if err := worker.Recover(t.Context()); err == nil {
		t.Fatal("closed worker resumed recovery")
	}
	if service.worker != worker || service.creations != creations || service.preparation != preparation || service.approvals != approvals {
		t.Fatal("service dependencies were reconstructed after shutdown")
	}
}
