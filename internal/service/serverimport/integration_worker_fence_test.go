package serverimport_test

import (
	"database/sql"
	"testing"
)

func TestWorkerCannotCompleteImportWithUnfinishedItems(t *testing.T) {
	service, database, _ := archiveImportFixture(t)
	created, err := service.Create(t.Context(), CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
	if err != nil {
		t.Fatal(err)
	}
	unit, ok, err := service.ClaimForTest(t.Context())
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	service.FinishTaskForTest(t.Context(), unit)
	current, err := service.Get(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var state string
	if err := database.QueryRowContext(t.Context(), `SELECT state FROM jobs WHERE id=?`, created.JobID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if current.State != "RUNNING" || state != "RUNNING" {
		t.Fatalf("unfinished import completed: import=%s job=%s", current.State, state)
	}
}

func TestExpiredWorkerCannotWriteProgressIntoNewExecution(t *testing.T) {
	service, database, oldUnit, newUnit := replacementWorkerFixture(t)
	beforeImport, beforeJob := workerVersions(t, database, newUnit)
	service.ProgressForTest(t.Context(), oldUnit, "DISCOVERING", 0, 1)
	afterImport, afterJob := workerVersions(t, database, newUnit)
	if beforeImport != afterImport || beforeJob != afterJob {
		t.Fatalf("stale worker updated new execution: import %d -> %d job %d -> %d", beforeImport, afterImport, beforeJob, afterJob)
	}
}

func replacementWorkerFixture(t *testing.T) (*Service, *sql.DB, work, work) {
	t.Helper()
	service, database, _ := archiveImportFixture(t)
	created, err := service.Create(t.Context(), CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
	if err != nil {
		t.Fatal(err)
	}
	oldUnit, ok, err := service.ClaimForTest(t.Context())
	if err != nil || !ok {
		t.Fatalf("initial claim: %v %v", ok, err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE jobs SET max_attempts=attempt_count WHERE id=?`, created.JobID); err != nil {
		t.Fatal(err)
	}
	service.FailTaskForTest(t.Context(), oldUnit, "INTERNAL_ERROR")
	failed, err := service.Get(t.Context(), created.ID)
	if err != nil || failed.State != "FAILED" {
		t.Fatalf("failed execution: %+v %v", failed, err)
	}
	if _, err := service.Retry(t.Context(), created.ID, failed.Version, controlActorID); err != nil {
		t.Fatal(err)
	}
	newUnit, ok, err := service.ClaimForTest(t.Context())
	if err != nil || !ok {
		t.Fatalf("replacement claim: %v %v", ok, err)
	}
	return service, database, oldUnit, newUnit
}

func workerVersions(t *testing.T, database *sql.DB, unit work) (int64, int64) {
	t.Helper()
	var importVersion, jobVersion int64
	if err := database.QueryRowContext(t.Context(), `SELECT import.version,job.version FROM server_imports import JOIN jobs job ON job.id=import.job_id WHERE import.id=?`, unit.ImportID).Scan(&importVersion, &jobVersion); err != nil {
		t.Fatal(err)
	}
	return importVersion, jobVersion
}

func TestReclaimedWorkerCannotInstallPersistedCandidate(t *testing.T) {
	fixture := persistedWorkerCandidate(t)
	service, database, created, oldUnit, selected := fixture.service, fixture.database, fixture.created, fixture.unit, fixture.selected
	if _, err := database.ExecContext(t.Context(), `UPDATE jobs SET leased_until_ms=0 WHERE id=?`, created.JobID); err != nil {
		t.Fatal(err)
	}
	newUnit, ok, err := service.ClaimForTest(t.Context())
	if err != nil || !ok {
		t.Fatalf("reclaim: %v %v", ok, err)
	}
	beforeImport, beforeJob := workerVersions(t, database, newUnit)
	service.CommitCandidateForTest(t.Context(), oldUnit, selected.Item, selected)
	var installed int64
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM bios_installations`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	afterImport, afterJob := workerVersions(t, database, newUnit)
	if installed != 0 || beforeImport != afterImport || beforeJob != afterJob {
		t.Fatalf("reclaimed worker installed BIOS: count=%d import=%d/%d job=%d/%d", installed, beforeImport, afterImport, beforeJob, afterJob)
	}
	var state string
	if err := database.QueryRowContext(t.Context(), `SELECT state FROM server_bios_import_items WHERE server_import_id=?`, created.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "EVALUATING" {
		t.Fatalf("stale installation changed outcome to %s", state)
	}
}

type workerCandidateFixture struct {
	service  *Service
	database *sql.DB
	created  Summary
	unit     work
	selected *evaluatedCandidate
}

func persistedWorkerCandidate(t *testing.T) workerCandidateFixture {
	t.Helper()
	service, database, rootPath := archiveImportFixture(t)
	writeArchiveCandidate(t, rootPath, true)
	created, err := service.Create(t.Context(), CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
	if err != nil {
		t.Fatal(err)
	}
	oldUnit, ok, err := service.ClaimForTest(t.Context())
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	items, err := service.LoadItemsForTest(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := openSelectedDirectory(rootPath, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := directory.Close(); err != nil {
			t.Error(err)
		}
	}()
	groups, ok := service.ExecuteDiscoveryForTest(t.Context(), oldUnit, directory, items)
	if !ok {
		t.Fatal("discovery failed")
	}
	values := rankCandidates(groups[items[0].RequirementID])
	if len(values) != 1 {
		t.Fatalf("eligible candidates: %d", len(values))
	}
	selected, err := service.VerifySelectedForTest(t.Context(), oldUnit, service.RootForTest("bios-root"), values[0])
	if err != nil {
		t.Fatal(err)
	}
	return workerCandidateFixture{service, database, created, oldUnit, selected}
}
