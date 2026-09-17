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

type scanCompletionFailure struct {
	*ScanPublication
	commit bool
}

func (repository scanCompletionFailure) WithScan(ctx context.Context, work func(emulationstationimportmodel.ScanScope) error) error {
	return repository.ScanPublication.WithScan(ctx, func(scope emulationstationimportmodel.ScanScope) error {
		if err := work(scope); err != nil {
			return err
		}
		if !repository.commit {
			return errLeaseStorage
		}
		records, ok := scope.Write.(scanRecords)
		if !ok {
			return errors.New("unexpected scan writer")
		}
		if _, err := records.executor.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
			return fmt.Errorf("defer scan fixture constraint: %w", err)
		}
		if _, err := records.executor.ExecContext(ctx, `INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES('missing-scan-parent',1,'{}','`+planDigest+`',12)`); err != nil {
			return fmt.Errorf("inject scan commit fault: %w", err)
		}
		return nil
	})
}

func TestScanFinalCallbackAndCommitFailuresRollBack(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"headers", "items", "finish", "reset", "reject"} {
		for _, commit := range []bool{false, true} {
			t.Run(operation+map[bool]string{false: "/callback", true: "/commit"}[commit], func(t *testing.T) {
				t.Parallel()
				db, unit, value := scanDatabase(t)
				prepareScanOperation(t, db, unit, value, operation)
				before := planRows(t, db)
				service := emulationstationimportservice.NewScanPublication(scanCompletionFailure{ScanPublication: NewScanPublication(db), commit: commit}, func() time.Time { return time.UnixMilli(1001) })
				err := runScanOperation(t.Context(), service, unit, value, operation)
				if err == nil {
					t.Fatal("late failure committed")
				}
				if !commit && !errors.Is(err, errLeaseStorage) {
					t.Fatalf("callback cause=%v", err)
				}
				if commit && !strings.Contains(err.Error(), "FOREIGN KEY") {
					t.Fatalf("commit cause=%v", err)
				}
				if !reflect.DeepEqual(before, planRows(t, db)) {
					t.Fatal("late failure retained scan writes")
				}
			})
		}
	}
}

func TestScanBeginCancellationAndReadFailurePreserveCauses(t *testing.T) {
	t.Parallel()
	db, unit, value := scanDatabase(t)
	before := planRows(t, db)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := scanService(db).Headers(ctx, unit, value); !errors.Is(err, context.Canceled) {
		t.Fatalf("context cause=%v", err)
	}
	if !reflect.DeepEqual(before, planRows(t, db)) {
		t.Fatal("cancelled transaction wrote projection")
	}
	var hits atomic.Int64
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
		if query == leaseSnapshotSQL+` AND job.id=?` && len(args) == 1 && args[0].Value == unit.JobID {
			hits.Add(1)
			return errLeaseStorage
		}
		return nil
	}})
	if err := scanService(faultDB).Headers(t.Context(), unit, value); !errors.Is(err, errLeaseStorage) || hits.Load() != 1 {
		t.Fatalf("read cause=%v hits=%d", err, hits.Load())
	}
	if !reflect.DeepEqual(before, planRows(t, db)) {
		t.Fatal("failed read wrote projection")
	}
}
