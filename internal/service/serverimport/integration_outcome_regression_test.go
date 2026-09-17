package serverimport_test

import (
	"math"
	model "retrom/internal/model/serverimport"
	"testing"

	"retrom/internal/capability/content/firmware"
	jobpersistence "retrom/internal/repo/jobs"
	jobservice "retrom/internal/service/jobs"
)

func TestItemOutcomeRejectsUnencodableEvidence(t *testing.T) {
	service, database, unit, candidate := discoveryWriteFixture(t)
	candidate.DAT = &firmware.DATEvaluation{Status: "HASH_WARNING", Method: "DAT_PARTIAL_FALLBACK"}
	if err := service.PersistCandidatesForTest(t.Context(), unit, map[string][]*evaluatedCandidate{candidate.Item.RequirementID: {candidate}}, walkCounts{}); err != nil {
		t.Fatal(err)
	}
	candidate.Details["bad"] = math.NaN()
	service.CompleteItemForTest(t.Context(), unit, candidate.Item.RequirementID, "COMMIT_FAILED", candidate, "INTERNAL_ERROR")
	var state string
	if err := database.QueryRowContext(t.Context(), `SELECT state FROM server_bios_import_items WHERE server_import_id=?`, unit.ImportID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "EVALUATING" {
		t.Fatalf("unencodable outcome committed: %s", state)
	}
}

func TestRepeatedItemOutcomeDoesNotAppendDuplicateEvent(t *testing.T) {
	service, database, unit, candidate := discoveryWriteFixture(t)
	service.CompleteItemForTest(t.Context(), unit, candidate.Item.RequirementID, "NOT_FOUND", nil, "BIOS_CANDIDATE_NOT_FOUND")
	service.CompleteItemForTest(t.Context(), unit, candidate.Item.RequirementID, "NOT_FOUND", nil, "BIOS_CANDIDATE_NOT_FOUND")
	var events int64
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM job_events WHERE job_id=? AND event_type='PROGRESS'`, unit.JobID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("repeated outcome appended %d events", events)
	}
}

func TestGenericJobCancellationPreservesCompletedImportCounts(t *testing.T) {
	legacy, database, _ := archiveImportFixture(t)
	created, err := legacy.Create(t.Context(), model.CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE server_bios_import_items SET state='NOT_FOUND',outcome_code='BIOS_CANDIDATE_NOT_FOUND',completed_at_ms=?,updated_at_ms=? WHERE server_import_id=?`, created.UpdatedAtMS, created.UpdatedAtMS, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE server_imports SET evaluated_item_count=1,not_found_count=1 WHERE id=?`, created.ID); err != nil {
		t.Fatal(err)
	}
	jobs := jobservice.New(jobpersistence.New(database), legacy.NowForTest)
	result, pending, err := jobs.Cancel(t.Context(), created.JobID, 1, "stop")
	if err != nil || pending || result.State != "CANCELLED" {
		t.Fatalf("job cancellation: %+v %v %v", result, pending, err)
	}
	current, err := legacy.Get(t.Context(), created.ID)
	if err != nil || current.State != "CANCELLED" || current.Counts.NotFound != 1 || current.Counts.Cancelled != 0 {
		t.Fatalf("cancelled import counts: %+v %v", current, err)
	}
}
