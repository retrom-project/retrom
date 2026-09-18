package emulationstationimport

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

func TestScanFinalCallbackAndCommitFailuresRollBack(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"headers", "items", "finish", "reset", "reject"} {
		for _, commit := range []bool{false, true} {
			t.Run(operation+map[bool]string{false: "/callback", true: "/commit"}[commit], func(t *testing.T) {
				t.Parallel()
				assertScanFailureRollback(t, operation, commit)
			})
		}
	}
}

func assertScanFailureRollback(t *testing.T, operation string, commit bool) {
	t.Helper()
	db, unit, value := scanDatabase(t)
	prepareScanOperation(t, db, unit, value, operation)
	before := planRows(t, db)
	repo := NewScanPublication(db)
	installScanFailureHook(t, repo, commit)
	service := emulationstationimportservice.NewScanPublication(
		repo, func() time.Time { return time.UnixMilli(1001) },
	)
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
}

func installScanFailureHook(
	t *testing.T, repo *ScanPublication, commit bool,
) {
	t.Helper()
	if !commit {
		repo.WithPreCommitHook(func(_ dbexec.Executor) error {
			return errLeaseStorage
		})
		return
	}
	repo.WithPreCommitHook(func(tx dbexec.Executor) error {
		if _, err := tx.ExecContext(
			t.Context(), `PRAGMA defer_foreign_keys=ON`,
		); err != nil {
			return fmt.Errorf("defer scan fixture constraint: %w", err)
		}
		if _, err := tx.ExecContext(
			t.Context(),
			`INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES('missing-scan-parent',1,'{}','`+planDigest+`',12)`,
		); err != nil {
			return fmt.Errorf("inject scan commit fault: %w", err)
		}
		return nil
	})
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
	assertScanReadFaultPreservation(t, db, unit, value, before)
}

func assertScanReadFaultPreservation(
	t *testing.T,
	db *sql.DB,
	unit emulationstationimportmodel.Execution,
	value emulationstationimportmodel.ScanProjection,
	before map[string]string,
) {
	t.Helper()
	var hits atomic.Int64
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			if query == leaseSnapshotSQL+` AND job.id=?` && len(args) == 1 && args[0].Value == unit.JobID {
				hits.Add(1)
				return errLeaseStorage
			}
			return nil
		},
	})
	if err := scanService(faultDB).Headers(t.Context(), unit, value); !errors.Is(err, errLeaseStorage) || hits.Load() != 1 {
		t.Fatalf("read cause=%v hits=%d", err, hits.Load())
	}
	if !reflect.DeepEqual(before, planRows(t, db)) {
		t.Fatal("failed read wrote projection")
	}
}
