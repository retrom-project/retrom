package serverimport_test

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/serverimport"

	importpersistence "retrom/internal/repo/serverimport"
	importservice "retrom/internal/service/serverimport"
)

type failingDiscoveryRepository struct {
	model.DiscoveryRepository
}

func (repository failingDiscoveryRepository) CommitWrite(ctx context.Context, work func(model.DiscoveryRecords) error) error {
	return repository.DiscoveryRepository.CommitWrite(ctx, func(records model.DiscoveryRecords) error {
		if err := work(records); err != nil {
			return err
		}
		return context.Canceled
	})
}

func TestDiscoveryWritesRollbackAfterLateFailure(t *testing.T) {
	legacy, database, unit, candidate := discoveryWriteFixture(t)
	groups := map[string][]*evaluatedCandidate{candidate.Item.RequirementID: {candidate}}
	service := importservice.NewDiscovery(failingDiscoveryRepository{importpersistence.NewDiscovery(database)}, legacy.NowForTest)
	beforeImport, beforeJob := workerVersions(t, database, unit)
	if err := service.Persist(t.Context(), unit, groups, walkCounts{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("persist late failure: %v", err)
	}
	afterImport, afterJob := workerVersions(t, database, unit)
	if beforeImport != afterImport || beforeJob != afterJob {
		t.Fatal("discovery failure changed execution versions")
	}
	var candidates int64
	var state string
	if err := database.QueryRowContext(t.Context(), `SELECT state,(SELECT count(*) FROM server_bios_import_candidates WHERE server_import_id=?) FROM server_bios_import_items WHERE server_import_id=?`, unit.ImportID, unit.ImportID).Scan(&state, &candidates); err != nil {
		t.Fatal(err)
	}
	if state != "PENDING" || candidates != 0 {
		t.Fatalf("partial evidence write: %s count=%d", state, candidates)
	}
	if err := legacy.PersistCandidatesForTest(t.Context(), unit, groups, walkCounts{}); err != nil {
		t.Fatal(err)
	}
	if err := service.Reset(t.Context(), unit); !errors.Is(err, context.Canceled) {
		t.Fatalf("reset late failure: %v", err)
	}
	if err := database.QueryRowContext(t.Context(), `SELECT state,(SELECT count(*) FROM server_bios_import_candidates WHERE server_import_id=?) FROM server_bios_import_items WHERE server_import_id=?`, unit.ImportID, unit.ImportID).Scan(&state, &candidates); err != nil {
		t.Fatal(err)
	}
	if state != "EVALUATING" || candidates != 1 {
		t.Fatalf("partial reset: %s count=%d", state, candidates)
	}
}
