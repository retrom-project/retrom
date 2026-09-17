package emulationstationimport

import (
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	application "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

func TestCancellationEventAndAuditRowCountFailuresRollback(t *testing.T) {
	t.Parallel()
	for _, statement := range []string{"INSERT INTO job_events(", "INSERT INTO audit_events("} {
		t.Run(statement, func(t *testing.T) {
			t.Parallel()
			db, summary := workflowDatabase(t, false)
			before := planRows(t, db)
			var hits atomic.Int64
			faultDB := testsupport.OpenSQLFaultDatabase(t, db, scanFaultHooks(statement, summary.ID, true, &hits))
			result, pending, err := application.NewWorkflowControl(NewWorkflowControl(faultDB, nil), nil, func() time.Time { return time.UnixMilli(20) }).Cancel(t.Context(), summary.ID, summary.Version, "Stop", "actor")
			if !errors.Is(err, errLeaseStorage) || hits.Load() != 1 || result.ID != "" || pending {
				t.Fatalf("cancel=%#v pending=%v cause=%v hits=%d", result, pending, err, hits.Load())
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("failed cancellation event retained durable changes")
			}
		})
	}
}
