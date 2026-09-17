package emulationstationimport

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

var errWorkflowStep = errors.New("workflow step failed")

type workflowFaultRepository struct {
	*WorkflowControl
	phase string
}

func (repository workflowFaultRepository) injectDBFault(ctx context.Context) {
	db := repository.WorkflowControl.database
	switch repository.phase {
	case "job CAS":
		db.ExecContext(ctx, `UPDATE jobs SET version=version+100`)
	case "plan CAS":
		db.ExecContext(ctx, `UPDATE emulationstation_imports SET version=version+100`)
	case "audit SQL":
		db.ExecContext(ctx,
			`INSERT INTO audit_events(id,actor_kind,actor_user_id,action,resource_type,resource_id,before_json,after_json,created_at_ms) VALUES('conflict-audit','USER','x','CANCEL','EMULATIONSTATION_IMPORT','x','{}','{}',1)`)
	case "response SQL":
		db.ExecContext(ctx,
			`ALTER TABLE emulationstation_imports RENAME COLUMN root_label_snapshot TO broken_root_label`)
	}
}

func (repository workflowFaultRepository) CommitCancelWorkflow(
	ctx context.Context, cmd emulationstationimportmodel.CancelWorkflowCommand,
) (emulationstationimportmodel.WorkflowSnapshot, bool, error) {
	if repository.phase == "callback" {
		return emulationstationimportmodel.WorkflowSnapshot{}, false, errWorkflowStep
	}
	repository.injectDBFault(ctx)
	return repository.WorkflowControl.CommitCancelWorkflow(ctx, cmd)
}

func (repository workflowFaultRepository) CommitRetryWorkflow(
	ctx context.Context, cmd emulationstationimportmodel.RetryWorkflowCommand,
) (emulationstationimportmodel.Summary, error) {
	if repository.phase == "callback" {
		return emulationstationimportmodel.Summary{}, errWorkflowStep
	}
	repository.injectDBFault(ctx)
	return repository.WorkflowControl.CommitRetryWorkflow(ctx, cmd)
}

type workflowAffectedExecutor struct{ dbexec.Executor }

func (executor workflowAffectedExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := executor.Executor.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(query, "UPDATE jobs") {
		return workflowAffectedResult{Result: result}, nil
	}
	return result, nil
}

type workflowAffectedResult struct{ sql.Result }

func (workflowAffectedResult) RowsAffected() (int64, error) { return 0, errWorkflowStep }

func assertWorkflowFault(t *testing.T, operation, phase string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s/%s expected error but got nil", operation, phase)
	}
	if phase == "callback" && !errors.Is(err, errWorkflowStep) {
		t.Fatalf("lost cause: %v", err)
	}
}

func TestWorkflowRollsBackAllProjectionsAtEveryWriteBoundary(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"cancel", "retry"} {
		for _, phase := range []string{"callback"} {
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
	service := emulationstationimportservice.NewWorkflowControl(
		workflowFaultRepository{
			WorkflowControl: NewWorkflowControl(db, nil), phase: phase,
		},
		verifiedStartSource{database: db},
		func() time.Time { return time.UnixMilli(12) },
	)
	var result emulationstationimportmodel.Summary
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
