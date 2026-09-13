package serverimport_test

import (
	"context"
	"errors"
	"testing"

	importpersistence "retrom/internal/repo/serverimport"
	importservice "retrom/internal/service/serverimport"
)

type failingCreationRepository struct {
	importservice.CreationRepository
}

func (repository failingCreationRepository) WithCreate(ctx context.Context, work func(importservice.CreationWriter) error) error {
	return repository.CreationRepository.WithCreate(ctx, func(writer importservice.CreationWriter) error {
		if err := work(writer); err != nil {
			return err
		}
		return context.Canceled
	})
}

func TestCreationLateFailureRollsBackTaskSnapshotItemsAndEvidence(t *testing.T) {
	legacy, database, _ := archiveImportFixture(t)
	repository := failingCreationRepository{importpersistence.NewCreation(database)}
	creation := importservice.NewCreation(repository, legacy.SourceSelectorForTest(), legacy.NowForTest)
	result, err := creation.Create(t.Context(), CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
	if !errors.Is(err, context.Canceled) || result.ID != "" {
		t.Fatalf("late creation failure: %+v %v", result, err)
	}
	var imports, jobs, inputs, items, events, audits int64
	err = database.QueryRowContext(t.Context(), `SELECT
(SELECT count(*) FROM server_imports),
(SELECT count(*) FROM jobs WHERE kind='SERVER_BIOS_IMPORT'),
(SELECT count(*) FROM job_input_snapshots),
(SELECT count(*) FROM server_bios_import_items),
(SELECT count(*) FROM job_events WHERE scope_type='SERVER_IMPORT'),
(SELECT count(*) FROM audit_events WHERE action='SERVER_IMPORT_CREATED')`).Scan(&imports, &jobs, &inputs, &items, &events, &audits)
	if err != nil {
		t.Fatal(err)
	}
	if imports != 0 || jobs != 0 || inputs != 0 || items != 0 || events != 0 || audits != 0 {
		t.Fatalf("partial creation: imports=%d jobs=%d inputs=%d items=%d events=%d audits=%d", imports, jobs, inputs, items, events, audits)
	}
}

func TestCreateConflictDoesNotLeaveOrphanJob(t *testing.T) {
	service, database, _ := archiveImportFixture(t)
	request := CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}
	created, err := service.Create(t.Context(), request, controlActorID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Create(t.Context(), request, controlActorID)
	if !errors.Is(err, ErrActive) || result.ID != "" {
		t.Fatalf("duplicate import: %+v %v", result, err)
	}
	var jobs, snapshots int64
	err = database.QueryRowContext(t.Context(), `SELECT count(*),
(SELECT count(*) FROM job_input_snapshots input JOIN jobs job ON job.id=input.job_id WHERE job.kind='SERVER_BIOS_IMPORT')
FROM jobs WHERE kind='SERVER_BIOS_IMPORT'`).Scan(&jobs, &snapshots)
	if err != nil || jobs != 1 || snapshots != 1 {
		t.Fatalf("conflict left jobs=%d snapshots=%d: %v", jobs, snapshots, err)
	}
	current, err := service.Get(t.Context(), created.ID)
	if err != nil || current.Version != created.Version || current.State != "QUEUED" {
		t.Fatalf("conflict changed existing import: %+v %v", current, err)
	}
}
