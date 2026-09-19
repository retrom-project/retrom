package emulationstationimport

import (
	"errors"
	"reflect"
	"testing"

	application "retrom/internal/model/emulationstationimport"
)

func TestReviewHandoffRejectsStaleIdentity(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"worker", "execution", "attempt"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			db, unit := itemWorkDatabase(t)
			before := planRows(t, db)
			request := application.ReviewHandoffRequest{
				Execution: unit, ItemID: "source-0",
				LibraryJobID: "lib-job", LibraryItemID: "lib-item",
			}
			switch field {
			case "worker":
				request.Execution.WorkerID = "other-worker"
			case "execution":
				request.Execution.ExecutionNo = 999
			case "attempt":
				request.Execution.Attempt = 999
			}
			label := "release-setup"
			err := NewReviewHandoff(db).CommitReviewHandoff(
				t.Context(), request, 1100, "audit-id", "SYSTEM", nil, &label,
			)
			if !errors.Is(err, application.ErrVersionConflict) {
				t.Fatalf("stale %s error=%v", field, err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatalf("stale %s changed rows", field)
			}
		})
	}
}

func TestReviewHandoffRejectsExpiredLease(t *testing.T) {
	t.Parallel()
	db, unit := itemWorkDatabase(t)
	if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET leased_until_ms=1 WHERE id=?`, unit.JobID); err != nil {
		t.Fatal(err)
	}
	before := planRows(t, db)
	label := "release-setup"
	err := NewReviewHandoff(db).CommitReviewHandoff(
		t.Context(),
		application.ReviewHandoffRequest{
			Execution: unit, ItemID: "source-0",
			LibraryJobID: "lib-job", LibraryItemID: "lib-item",
		},
		1100, "audit-id", "SYSTEM", nil, &label,
	)
	if !errors.Is(err, application.ErrVersionConflict) {
		t.Fatalf("expired lease error=%v", err)
	}
	if !reflect.DeepEqual(before, planRows(t, db)) {
		t.Fatal("expired lease changed rows")
	}
}
