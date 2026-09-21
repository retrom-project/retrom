package emulationstationimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"retrom/internal/dbexec"
	application "retrom/internal/service/emulationstationimport"
)

var errWorkflowStep = errors.New("workflow step failed")

type workflowFaultRepository struct {
	*WorkflowControl
	phase string
}

func (repository workflowFaultRepository) WithControl(ctx context.Context, work func(application.WorkflowScope) error) error {
	return repository.WorkflowControl.WithControl(ctx, func(scope application.WorkflowScope) error {
		records, ok := scope.Write.(workflowRecords)
		if !ok {
			return errors.New("unexpected workflow records")
		}
		if repository.phase == "affected rows" {
			records.executor = workflowAffectedExecutor{Executor: records.executor}
		}
		scope.Write = workflowFaultWriter{WorkflowWriter: records, executor: records.executor, phase: repository.phase}
		return work(scope)
	})
}

type workflowFaultWriter struct {
	application.WorkflowWriter
	executor dbexec.Executor
	phase    string
}

func (writer workflowFaultWriter) Cancel(ctx context.Context, plan application.CancellationPlan) error {
	switch writer.phase {
	case "job CAS":
		plan.Before.JobVersion++
	case "plan CAS":
		plan.Before.Summary.Version++
	case "audit SQL":
		plan.AuditID = "audit-0"
	}
	if err := writer.WorkflowWriter.Cancel(ctx, plan); err != nil {
		return err
	}
	return writer.afterWrite(ctx)
}

func (writer workflowFaultWriter) Retry(ctx context.Context, plan application.RetryPlan) error {
	switch writer.phase {
	case "job CAS":
		plan.Before.JobVersion++
	case "plan CAS":
		plan.Before.Summary.Version++
	case "mapping CAS":
		plan.Before.Summary.MappingVersion++
	case "root CAS":
		plan.Before.RootConfigDigest = "changed"
	case "year CAS":
		plan.Before.ReleaseYearMax++
	case "audit SQL":
		plan.AuditID = "audit-0"
	case "input SQL":
		plan.Execution = 1
	case "item count":
		plan.Before.RetryableItems++
	}
	if err := writer.WorkflowWriter.Retry(ctx, plan); err != nil {
		return err
	}
	return writer.afterWrite(ctx)
}

func (writer workflowFaultWriter) afterWrite(ctx context.Context) error {
	if writer.phase == "callback" {
		return errWorkflowStep
	}
	statement := `ALTER TABLE emulationstation_imports RENAME COLUMN root_label_snapshot TO broken_root_label`
	if writer.phase == "commit FK" {
		statement = `PRAGMA defer_foreign_keys=ON;
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES('missing-workflow-parent',1,'{}','` + planDigest + `',12)`
	}
	if _, err := writer.executor.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("inject workflow fault: %w", err)
	}
	return nil
}

type workflowAffectedExecutor struct{ dbexec.Executor }

func (executor workflowAffectedExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := executor.Executor.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("execute workflow test statement: %w", err)
	}
	if strings.HasPrefix(query, "UPDATE jobs") {
		return workflowAffectedResult{Result: result}, nil
	}
	return result, nil
}

type workflowAffectedResult struct{ sql.Result }

func (workflowAffectedResult) RowsAffected() (int64, error) { return 0, errWorkflowStep }
func TestWorkflowRollsBackAllProjectionsAtEveryWriteBoundary(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"cancel", "retry"} {
		phases := []string{"job CAS", "plan CAS", "audit SQL", "affected rows", "callback", "response SQL", "commit FK"}
		if operation == "retry" {
			phases = append(phases, "mapping CAS", "root CAS", "year CAS", "input SQL", "item count")
		}
		for _, phase := range phases {
			t.Run(operation+"/"+phase, func(t *testing.T) {
				t.Parallel()
				assertWorkflowRollback(t, operation, phase)
			})
		}
	}
}

func assertWorkflowRollback(t *testing.T, operation, phase string) {
	t.Helper()
	db, before := workflowDatabase(t, operation == "retry")
	rows := planRows(t, db)
	service := application.NewWorkflowControl(workflowFaultRepository{WorkflowControl: NewWorkflowControl(db), phase: phase}, verifiedStartSource{database: db}, func() time.Time { return time.UnixMilli(12) })
	var result application.Summary
	var err error
	var pending bool
	if operation == "cancel" {
		result, pending, err = service.Cancel(t.Context(), before.ID, before.Version, "Stop", mappingActor)
	} else {
		result, err = service.Retry(t.Context(), before.ID, before.Version, mappingActor)
	}
	if err == nil || result.ID != "" || pending {
		t.Fatalf("partial %s %s result=%#v pending=%v error=%v", operation, phase, result, pending, err)
	}
	assertWorkflowFault(t, operation, phase, err)
	if !reflect.DeepEqual(planRows(t, db), rows) {
		t.Fatal("failed workflow changed job/input/item/counters/event/audit/Tag/payload")
	}
}

func assertWorkflowFault(t *testing.T, operation, phase string, err error) {
	t.Helper()
	if strings.HasSuffix(phase, "CAS") || phase == "item count" {
		want := application.ErrNotRetryable
		if operation == "cancel" {
			want = application.ErrNotCancellable
		}
		if !errors.Is(err, want) {
			t.Fatalf("wrong CAS failure: %v", err)
		}
	}
	if (phase == "affected rows" || phase == "callback") && !errors.Is(err, errWorkflowStep) {
		t.Fatalf("lost cause: %v", err)
	}
	fragments := map[string]string{
		"audit SQL":    "audit_events.id",
		"input SQL":    "job_input_snapshots.job_id",
		"response SQL": "root_label_snapshot",
		"commit FK":    "FOREIGN KEY constraint failed",
	}
	if fragment, ok := fragments[phase]; ok && !strings.Contains(err.Error(), fragment) {
		t.Fatalf("%s did not reach real SQL failure %q: %v", phase, fragment, err)
	}
}
