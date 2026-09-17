package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

func TestWorkflowRetryRechecksRealStorageAfterSourceIO(t *testing.T) {
	t.Parallel()
	for _, change := range []struct {
		name, statement string
		want            error
	}{
		{"plan", `UPDATE emulationstation_imports SET version=version+1`, emulationstationimportmodel.ErrNotRetryable},
		{"job", `UPDATE jobs SET version=version+1 WHERE kind='SERVER_EMULATIONSTATION_IMPORT'`, emulationstationimportmodel.ErrNotRetryable},
		{"execution", `UPDATE jobs SET execution_no=execution_no+1 WHERE kind='SERVER_EMULATIONSTATION_IMPORT'`, emulationstationimportmodel.ErrNotRetryable},
		{"mapping", `UPDATE emulationstation_imports SET mapping_version=mapping_version+1`, emulationstationimportmodel.ErrNotRetryable},
		{"target", `UPDATE platform_instances SET version=version+1`, emulationstationimportmodel.ErrMappingTargetChanged},
		{"source", `UPDATE emulationstation_imports SET source_snapshot_digest='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'`, emulationstationimportmodel.ErrSourceChanged},
		{"year", `UPDATE emulationstation_imports SET release_year_max=release_year_max+1`, emulationstationimportmodel.ErrSourceChanged},
	} {
		t.Run(change.name, func(t *testing.T) {
			t.Parallel()
			db, before := workflowDatabase(t, true)
			var rows map[string]string
			source := changingStartSource{verifiedStartSource: verifiedStartSource{database: db}, change: func() error {
				if _, err := db.ExecContext(t.Context(), change.statement); err != nil {
					return fmt.Errorf("change workflow fixture: %w", err)
				}
				rows = planRows(t, db)
				return nil
			}}
			result, err := emulationstationimportservice.NewWorkflowControl(NewWorkflowControl(db), source, func() time.Time { return time.UnixMilli(12) }).Retry(t.Context(), before.ID, before.Version, mappingActor)
			if !errors.Is(err, change.want) || result.ID != "" {
				t.Fatalf("%s result=%#v error=%v", change.name, result, err)
			}
			if !reflect.DeepEqual(rows, planRows(t, db)) {
				t.Fatal("stale retry wrote projections")
			}
		})
	}
}

type concurrentWorkflowSource struct {
	verifiedStartSource
	arrived chan<- struct{}
	resume  <-chan struct{}
}

func (source concurrentWorkflowSource) VerifyGamelists(ctx context.Context, _, _ string, _ []emulationstationimportmodel.GamelistEvidence) error {
	select {
	case source.arrived <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("reach workflow barrier: %w", ctx.Err())
	}
	select {
	case <-source.resume:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait workflow barrier: %w", ctx.Err())
	}
}

func TestWorkflowConcurrentRetriesCreateOnlyOneNewExecution(t *testing.T) {
	t.Parallel()
	db, before := workflowDatabase(t, true)
	arrived, resume := make(chan struct{}, 2), make(chan struct{})
	source := concurrentWorkflowSource{verifiedStartSource: verifiedStartSource{database: db}, arrived: arrived, resume: resume}
	service := emulationstationimportservice.NewWorkflowControl(NewWorkflowControl(db), source, func() time.Time { return time.UnixMilli(12) })
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := service.Retry(t.Context(), before.ID, before.Version, mappingActor); results <- err }()
	}
	for range 2 {
		select {
		case <-arrived:
		case err := <-results:
			t.Fatalf("retry failed before source barrier: %v", err)
		}
	}
	close(resume)
	succeeded, conflicted := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, emulationstationimportmodel.ErrNotRetryable):
			conflicted++
		default:
			t.Fatalf("unexpected competing retry: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("success=%d conflicts=%d", succeeded, conflicted)
	}
	var execution, snapshots, events, audits int
	if err := db.QueryRowContext(t.Context(), `SELECT execution_no,
(SELECT count(*) FROM job_input_snapshots WHERE job_id=jobs.id),
(SELECT count(*) FROM job_events WHERE job_id=jobs.id AND event_type='MANUAL_RETRY'),
(SELECT count(*) FROM audit_events WHERE resource_id='import-0' AND action='EMULATIONSTATION_IMPORT_RETRIED')
FROM jobs WHERE id=?`, *before.ImportJobID).Scan(&execution, &snapshots, &events, &audits); err != nil {
		t.Fatal(err)
	}
	if execution != 2 || snapshots != 2 || events != 1 || audits != 1 {
		t.Fatalf("execution=%d snapshots=%d events=%d audits=%d", execution, snapshots, events, audits)
	}
}

func TestWorkflowRetryPreservesFrozenDeletedTagPolicy(t *testing.T) {
	t.Parallel()
	db, before := workflowDatabase(t, true)
	if _, err := db.ExecContext(t.Context(), `UPDATE tags SET status='DELETED',deleted_at_ms=12,version=version+1`); err != nil {
		t.Fatal(err)
	}
	tags, relations := planTable(t, db, "tags"), planTable(t, db, "emulationstation_collection_tags")
	_, err := emulationstationimportservice.NewWorkflowControl(NewWorkflowControl(db), verifiedStartSource{database: db}, func() time.Time { return time.UnixMilli(12) }).Retry(t.Context(), before.ID, before.Version, mappingActor)
	if err != nil {
		t.Fatal(err)
	}
	if tags != planTable(t, db, "tags") || relations != planTable(t, db, "emulationstation_collection_tags") {
		t.Fatal("retry changed frozen tags")
	}
}

func TestWorkflowRetryHonorsDiscardFenceAndRollsBackExecution(t *testing.T) {
	t.Parallel()
	db, before := workflowDatabase(t, true)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO import_batch_discards(kind,import_id,requested_by_user_id,state,requested_at_ms,updated_at_ms)
VALUES('EMULATIONSTATION',? ,?,'REQUESTED',12,12)`, before.ID, mappingActor); err != nil {
		t.Fatal(err)
	}
	rows := planRows(t, db)
	result, err := emulationstationimportservice.NewWorkflowControl(NewWorkflowControl(db), verifiedStartSource{database: db}, func() time.Time { return time.UnixMilli(12) }).Retry(t.Context(), before.ID, before.Version, mappingActor)
	if result.ID != "" || err == nil || !strings.Contains(err.Error(), "IMPORT_BATCH_DISCARDED") {
		t.Fatalf("discarded retry result=%#v error=%v", result, err)
	}
	if !reflect.DeepEqual(planRows(t, db), rows) {
		t.Fatal("discarded retry changed input, items, jobs, or audit")
	}
}
