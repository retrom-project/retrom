//go:build integration

package libraryimport

import (
	"testing"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func TestImportCreationFailureCannotMutateReplacementExecution(t *testing.T) {
	for _, column := range []string{"execution_no", "attempt_count"} {
		t.Run(column, func(t *testing.T) {
			service, plan := preparedCommitFixture(t)
			admissions := application.NewImportAdmissions(repository.NewImportAdmissions(service.database), nil, service.tags,
				application.ImportAdmissionOptions{Now: service.now})
			created, err := admissions.Queue(t.Context(), plan.Request)
			if err != nil {
				t.Fatal(err)
			}
			work, err := service.claimImportGroup(t.Context(), created.JobID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.database.ExecContext(t.Context(), `UPDATE jobs SET `+column+`=`+column+`+1 WHERE id=?`, created.JobID); err != nil {
				t.Fatal(err)
			}
			service.finishImportGroupFailure(t.Context(), work, ErrInvalid)
			var jobState, importState string
			if err := service.database.QueryRowContext(t.Context(), `
SELECT job.state,parent.state FROM jobs job JOIN import_jobs parent ON parent.id=job.scope_id WHERE job.id=?`, created.JobID).
				Scan(&jobState, &importState); err != nil {
				t.Fatal(err)
			}
			if jobState != "RUNNING" || importState != "RUNNING" {
				t.Fatalf("stale %s failure mutated replacement: job=%s import=%s", column, jobState, importState)
			}
		})
	}
}
