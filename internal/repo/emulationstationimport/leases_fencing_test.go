package emulationstationimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

type leaseConcurrentChange struct {
	repo     *Leases
	database *sql.DB
	scenario string
}

func (wrapper leaseConcurrentChange) LoadLeaseCandidate(
	ctx context.Context, now int64,
) (emulationstationimportmodel.LeaseSnapshot, bool, error) {
	return wrapper.repo.LoadLeaseCandidate(ctx, now)
}

func (wrapper leaseConcurrentChange) LoadCurrentLease(
	ctx context.Context, id string,
) (emulationstationimportmodel.LeaseSnapshot, bool, error) {
	return wrapper.repo.LoadCurrentLease(ctx, id)
}

func (wrapper leaseConcurrentChange) CommitLeaseClaim(
	ctx context.Context, change emulationstationimportmodel.ClaimLease,
) error {
	if err := wrapper.injectMutation(ctx, change.Before.JobID); err != nil {
		return err
	}
	return wrapper.repo.CommitLeaseClaim(ctx, change)
}

func (wrapper leaseConcurrentChange) CommitLeaseRenewal(
	ctx context.Context, change emulationstationimportmodel.RenewLease,
) error {
	if err := wrapper.injectMutation(ctx, change.Before.JobID); err != nil {
		return err
	}
	return wrapper.repo.CommitLeaseRenewal(ctx, change)
}

func (wrapper leaseConcurrentChange) injectMutation(
	ctx context.Context, jobID string,
) error {
	return injectLeaseReplacement(ctx, wrapper.database, wrapper.scenario, jobID)
}

func injectLeaseReplacement(
	ctx context.Context, executor dbexec.Executor, scenario, jobID string,
) error {
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
	if _, err := executor.ExecContext(ctx, queries[scenario], jobID); err != nil {
		return fmt.Errorf("inject replacement %s: %w", scenario, err)
	}
	return nil
}

func TestLeaseCASRollsBackReplacedTransactionSnapshot(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"claim", "renew"} {
		scenarios := []string{
			"job version", "plan version", "attempt", "execution",
			"deadline", "kind", "scope", "link",
		}
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
	if _, err := NewCreation(db).CommitCreation(
		t.Context(), creationPlan(1),
	); err != nil {
		t.Fatal(err)
	}
	var unit emulationstationimportmodel.Execution
	if operation == "renew" {
		value, found, err := emulationstationimportservice.NewLeases(
			NewLeases(db), func() time.Time { return time.UnixMilli(1000) },
		).Claim(t.Context())
		if err != nil || !found {
			t.Fatalf("claim=%v error=%v", found, err)
		}
		unit = value
	}
	wrapper := leaseConcurrentChange{
		repo: NewLeases(db), database: db, scenario: scenario,
	}
	service := emulationstationimportservice.NewLeases(
		wrapper, func() time.Time { return time.UnixMilli(1001) },
	)
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
	if !errors.Is(err, emulationstationimportmodel.ErrVersionConflict) {
		t.Fatalf("CAS=%v", err)
	}
}
