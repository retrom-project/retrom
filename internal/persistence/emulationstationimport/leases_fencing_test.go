package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"retrom/internal/persistence/dbexec"
	application "retrom/internal/service/emulationstationimport"
)

type leaseConcurrentChange struct {
	*Leases
	scenario string
}

func (repository leaseConcurrentChange) WithLease(ctx context.Context, work func(application.LeaseScope) error) error {
	return repository.Leases.WithLease(ctx, func(scope application.LeaseScope) error {
		records, ok := scope.Write.(leaseRecords)
		if !ok {
			return errors.New("unexpected lease scope")
		}
		scope.Write = leaseConcurrentWriter{LeaseWriter: records, executor: records.executor, scenario: repository.scenario}
		return work(scope)
	})
}

type leaseConcurrentWriter struct {
	application.LeaseWriter
	executor dbexec.Executor
	scenario string
}

func (writer leaseConcurrentWriter) Claim(ctx context.Context, change application.ClaimLease) error {
	if err := writer.replace(ctx, change.Before); err != nil {
		return err
	}
	return writer.LeaseWriter.Claim(ctx, change)
}

func (writer leaseConcurrentWriter) Renew(ctx context.Context, change application.RenewLease) error {
	if err := writer.replace(ctx, change.Before); err != nil {
		return err
	}
	return writer.LeaseWriter.Renew(ctx, change)
}

func (writer leaseConcurrentWriter) replace(ctx context.Context, before application.LeaseSnapshot) error {
	queries := map[string]string{
		"job version":  `UPDATE jobs SET version=version+1 WHERE id=?`,
		"plan version": `UPDATE emulationstation_imports SET version=version+1 WHERE scan_job_id=?`,
		"attempt":      `UPDATE jobs SET attempt_count=attempt_count+1 WHERE id=?`,
		"execution":    `UPDATE jobs SET execution_no=execution_no+1 WHERE id=?`,
		"deadline":     `UPDATE jobs SET execution_started_at_ms=20,execution_deadline_at_ms=900 WHERE id=?`,
		"kind":         `UPDATE jobs SET kind='SERVER_EMULATIONSTATION_IMPORT' WHERE id=?`,
		"scope":        `UPDATE jobs SET scope_id='import-1' WHERE id=?`,
		"link":         `UPDATE emulationstation_imports SET scan_job_id='other' WHERE scan_job_id=?`,
		"lease":        `UPDATE jobs SET leased_until_ms=1000 WHERE id=?`,
		"worker":       `UPDATE jobs SET worker_id='replacement' WHERE id=?`,
	}
	if _, err := writer.executor.ExecContext(ctx, queries[writer.scenario], before.JobID); err != nil {
		return fmt.Errorf("inject replacement %s: %w", writer.scenario, err)
	}
	return nil
}

func TestLeaseCASRollsBackReplacedTransactionSnapshot(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"claim", "renew"} {
		scenarios := []string{"job version", "plan version", "attempt", "execution", "deadline", "kind", "scope", "link"}
		if operation == "renew" {
			scenarios = append(scenarios, "lease", "worker")
		}
		for _, scenario := range scenarios {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				t.Parallel()
				assertLeaseCASRollback(t, operation, scenario)
			})
		}
	}
}

func assertLeaseCASRollback(t *testing.T, operation, scenario string) {
	t.Helper()
	db, _ := leaseDatabase(t, false)
	seedLeaseOrphan(t, db)
	if err := NewCreation(db).WithCreate(t.Context(), func(writer application.CreationWriter) error {
		_, err := writer.Insert(t.Context(), creationPlan(1))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var unit application.Execution
	if operation == "renew" {
		value, found, err := application.NewLeases(NewLeases(db), func() time.Time { return time.UnixMilli(1000) }).Claim(t.Context())
		if err != nil || !found {
			t.Fatalf("claim=%v error=%v", found, err)
		}
		unit = value
	}
	before := planRows(t, db)
	service := application.NewLeases(leaseConcurrentChange{Leases: NewLeases(db), scenario: scenario}, func() time.Time { return time.UnixMilli(1001) })
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
		if state != application.LeaseLost {
			t.Fatalf("partial renewal=%s", state)
		}
	}
	if !errors.Is(err, application.ErrVersionConflict) {
		t.Fatalf("CAS=%v", err)
	}
	if !reflect.DeepEqual(planRows(t, db), before) {
		t.Fatal("failed CAS retained partial changes")
	}
}
