package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var errLeaseStorage = errors.New("lease storage unavailable")

type leaseResultFault struct {
	driver.Result
	zero bool
}

func (result leaseResultFault) RowsAffected() (int64, error) {
	if result.zero {
		return 0, nil
	}
	return 0, errLeaseStorage
}

func TestLeaseSQLFailuresRollBackJobAggregateAndEvent(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"claim", "renew"} {
		stages := []string{"read", "job", "affected rows", "zero rows"}
		if operation == "claim" {
			stages = append(stages, "aggregate", "event")
		}
		for _, stage := range stages {
			t.Run(operation+"/"+stage, func(t *testing.T) {
				t.Parallel()
				assertLeaseSQLRollback(t, operation, stage)
			})
		}
	}
}

func assertLeaseSQLRollback(t *testing.T, operation, stage string) {
	t.Helper()
	db, id := leaseDatabase(t, false)
	var unit emulationstationimportmodel.Execution
	if operation == "renew" {
		value, found, err := emulationstationimportservice.NewLeases(NewLeases(db), func() time.Time { return time.UnixMilli(1000) }).Claim(t.Context())
		if err != nil || !found {
			t.Fatalf("claim=%v error=%v", found, err)
		}
		unit = value
	}
	before := planRows(t, db)
	var hits atomic.Int64
	hooks := leaseFaultHooks(stage, id, &hits)
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, hooks)
	service := emulationstationimportservice.NewLeases(NewLeases(faultDB), func() time.Time { return time.UnixMilli(1001) })
	var err error
	if operation == "claim" {
		value, found, callErr := service.Claim(t.Context())
		err = callErr
		if found || value.JobID != "" {
			t.Fatalf("partial claim=%#v found=%v", value, found)
		}
	} else {
		state, callErr := service.Renew(t.Context(), unit)
		err = callErr
		if state != emulationstationimportmodel.LeaseLost {
			t.Fatalf("partial renewal=%s", state)
		}
	}
	want := errLeaseStorage
	if stage == "zero rows" {
		want = emulationstationimportmodel.ErrVersionConflict
	}
	if !errors.Is(err, want) || hits.Load() != 1 {
		t.Fatalf("fault=%v hits=%d", err, hits.Load())
	}
	if !reflect.DeepEqual(planRows(t, db), before) {
		t.Fatal("failed lease changed durable state")
	}
}

func leaseFaultHooks(stage, id string, hits *atomic.Int64) testsupport.SQLFaultHooks {
	return testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if stage == "read" && strings.HasPrefix(query, leaseSnapshotSQL) {
				hits.Add(1)
				return errLeaseStorage
			}
			return nil
		},
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			prefixes := map[string]string{"job": "UPDATE jobs SET", "aggregate": "UPDATE emulationstation_imports SET", "event": "INSERT INTO job_events("}
			prefix, ok := prefixes[stage]
			if ok && strings.HasPrefix(strings.Join(strings.Fields(query), " "), prefix) && leaseBoundID(args, id, stage) {
				hits.Add(1)
				return errLeaseStorage
			}
			return nil
		},
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if (stage == "affected rows" || stage == "zero rows") && strings.HasPrefix(query, "UPDATE jobs SET") && leaseBoundID(args, id, stage) {
				hits.Add(1)
				return leaseResultFault{Result: result, zero: stage == "zero rows"}, nil
			}
			return result, nil
		},
	}
}

func leaseBoundID(args []driver.NamedValue, id, stage string) bool {
	if stage == "aggregate" {
		id = "import-0"
	}
	for _, arg := range args {
		if arg.Value == id {
			return true
		}
	}
	return false
}

type leaseCompletionFailure struct {
	*Leases
	stage string
}

func (repository leaseCompletionFailure) WithLease(ctx context.Context, work func(emulationstationimportmodel.LeaseScope) error) error {
	return repository.Leases.WithLease(ctx, func(scope emulationstationimportmodel.LeaseScope) error {
		if err := work(scope); err != nil {
			return err
		}
		if repository.stage == "callback" {
			return errLeaseStorage
		}
		records, ok := scope.Read.(leaseRecords)
		if !ok {
			return errors.New("unexpected lease scope")
		}
		if _, err := records.executor.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
			return fmt.Errorf("defer lease fixture constraint: %w", err)
		}
		_, err := records.executor.ExecContext(ctx, `INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES('missing-lease-parent',1,'{}','`+planDigest+`',12)`)
		if err != nil {
			return fmt.Errorf("inject deferred lease fault: %w", err)
		}
		return nil
	})
}

func TestLeaseCompletionFailuresReturnNoSuccessfulResponse(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"claim", "renew"} {
		for _, stage := range []string{"callback", "commit FK"} {
			t.Run(operation+"/"+stage, func(t *testing.T) {
				t.Parallel()
				assertLeaseCompletionFailure(t, operation, stage)
			})
		}
	}
}

func assertLeaseCompletionFailure(t *testing.T, operation, stage string) {
	t.Helper()
	db, _ := leaseDatabase(t, false)
	var unit emulationstationimportmodel.Execution
	if operation == "renew" {
		value, found, err := emulationstationimportservice.NewLeases(NewLeases(db), func() time.Time { return time.UnixMilli(1000) }).Claim(t.Context())
		if err != nil || !found {
			t.Fatalf("claim=%v %v", found, err)
		}
		unit = value
	}
	before := planRows(t, db)
	service := emulationstationimportservice.NewLeases(leaseCompletionFailure{Leases: NewLeases(db), stage: stage}, func() time.Time { return time.UnixMilli(1001) })
	var err error
	if operation == "claim" {
		value, found, callErr := service.Claim(t.Context())
		err = callErr
		if found || value.JobID != "" {
			t.Fatalf("partial claim=%#v", value)
		}
	} else {
		state, callErr := service.Renew(t.Context(), unit)
		err = callErr
		if state != emulationstationimportmodel.LeaseLost {
			t.Fatalf("partial renewal=%s", state)
		}
	}
	if err == nil {
		t.Fatal("completion failure committed")
	}
	if stage == "callback" && !errors.Is(err, errLeaseStorage) {
		t.Fatalf("lost cause: %v", err)
	}
	if stage == "commit FK" && !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("missing commit constraint: %v", err)
	}
	if !reflect.DeepEqual(planRows(t, db), before) {
		t.Fatal("completion failure changed durable state")
	}
}
