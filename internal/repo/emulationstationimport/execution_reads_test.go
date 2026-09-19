package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

func TestExecutionControlReadFailuresRetainCauseAndState(t *testing.T) {
	t.Parallel()
	for _, read := range []string{"owner", "progress", "reviews"} {
		t.Run(read, func(t *testing.T) {
			t.Parallel()
			db, unit := recoveryDatabase(t, true, false)
			before := planRows(t, db)
			var hits atomic.Int64
			faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
				if !executionReadMatches(read, query) {
					return nil
				}
				for _, arg := range args {
					if arg.Value == unit.JobID || arg.Value == unit.ImportID {
						hits.Add(1)
						return errLeaseStorage
					}
				}
				return nil
			}})
			service := application.NewExecutionControl(NewExecutionControl(faultDB), func() time.Time { return time.UnixMilli(1002) })
			state, err := service.Fail(t.Context(), unit, emulationstationimportmodel.ExecutionFailure{Code: "INTERNAL_ERROR"})
			if state != "" || !errors.Is(err, errLeaseStorage) || hits.Load() != 1 {
				t.Fatalf("read=%s state=%s cause=%v hits=%d", read, state, err, hits.Load())
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("read error changed execution")
			}
		})
	}
}

func executionReadMatches(read, query string) bool {
	switch read {
	case "owner":
		return query == leaseSnapshotSQL+` AND job.id=?`
	case "progress":
		return strings.Contains(query, "execution_state NOT IN ('PENDING','COPYING','VALIDATING')")
	case "reviews":
		return strings.Contains(query, "JOIN server_import_upload_owners owner")
	default:
		return false
	}
}
