package sourceimport

import (
	"errors"
	"fmt"
	"testing"

	repository "retrom/internal/persistence/sourceimport"
	application "retrom/internal/service/sourceimport"
)

func TestArcadeCompanionsRejectReplacedOwnerBeforeRegisteringCAS(t *testing.T) {
	t.Parallel()
	service, unit, root, item := arcadeCompanionFixture(t)
	if _, err := service.database.ExecContext(
		t.Context(),
		`UPDATE jobs SET worker_id='replacement' WHERE id='work'`,
	); err != nil {
		t.Fatal(err)
	}
	result, err := service.importExecutor(root).CompanionFiles(t.Context(), unit, item)
	if !errors.Is(err, ErrVersionConflict) || len(result) != 0 {
		t.Fatalf("old worker got companions=%#v err=%v", result, err)
	}
	var count int
	if err := service.database.QueryRowContext(t.Context(), `SELECT count(*) FROM blobs`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("old worker inserted %d blobs", count)
	}
}

func TestArcadeCompanionRechecksOwnerAndCandidateAfterPhysicalCopy(t *testing.T) {
	t.Parallel()
	for _, change := range []struct{ name, query string }{
		{"worker", `UPDATE jobs SET worker_id='replacement' WHERE id='work'`},
		{"lease", `UPDATE jobs SET leased_until_ms=10 WHERE id='work'`},
		{"path", `UPDATE source_import_item_files SET relative_path='changed.zip' WHERE item_id='parent'`},
		{"facts", `UPDATE source_import_item_files SET source_facts_digest='changed' WHERE item_id='parent'`},
		{"mapping", `UPDATE source_import_collections SET mapping_action='SKIP' WHERE id='collection'`},
		{"dependency", `DELETE FROM dat_machines WHERE machine_name='child'`},
	} {
		t.Run(change.name, func(t *testing.T) {
			service, unit, root, item := arcadeCompanionFixture(t)
			companions := application.NewCompanions(repository.NewCompanions(service.database), service.now)
			candidates, err := companions.Find(t.Context(), unit.Identity(), item.ID)
			if err != nil || len(candidates) != 1 {
				t.Fatalf("candidates=%v err=%v", candidates, err)
			}
			candidate := candidates[0]
			file := candidate.File
			metadata, err := service.copySource(t.Context(), root, unit.RelativePath, file.Path, file.Size, file.Facts)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.database.ExecContext(t.Context(), change.query); err != nil {
				t.Fatal(err)
			}
			blobID, err := companions.Record(t.Context(), unit.Identity(), item.ID, candidate, verifiedMaterial(metadata))
			if err == nil || blobID != "" {
				t.Fatalf("changed %s accepted blob=%s err=%v", change.name, blobID, err)
			}
			var count int
			if err := service.database.QueryRowContext(t.Context(), `SELECT count(*) FROM blobs`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("changed %s registered %d blobs", change.name, count)
			}
		})
	}
}

func TestArcadeCompanionClosureHandlesCycleAndDeepRelations(t *testing.T) {
	t.Parallel()
	service, unit, _, item := arcadeCompanionFixture(t)
	// UNION terminates cycles and the query has no 64-node cutoff.
	if _, err := service.database.ExecContext(
		t.Context(),
		`UPDATE dat_machines SET cloneof='depth-0' WHERE machine_name='child';UPDATE dat_machines SET romof='child' WHERE machine_name='parent'`,
	); err != nil {
		t.Fatal(err)
	}
	for index := range 70 {
		next := fmt.Sprintf("depth-%d", index+1)
		if index == 69 {
			next = "parent"
		}
		if _, err := service.database.ExecContext(
			t.Context(),
			`INSERT INTO dat_machines VALUES('dat',?,?,NULL)`,
			fmt.Sprintf("depth-%d", index),
			next,
		); err != nil {
			t.Fatal(err)
		}
	}
	companions := application.NewCompanions(repository.NewCompanions(service.database), service.now)
	result, err := companions.Find(t.Context(), unit.Identity(), item.ID)
	if err != nil || len(result) != 1 || result[0].File.Path != "parent.zip" {
		t.Fatalf("deep cyclic closure=%#v err=%v", result, err)
	}
}
