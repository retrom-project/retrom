package emulationstationimport

import (
	"testing"
	"time"
)

func TestESImportProcessingRejectsReplacedWorker(t *testing.T) {
	fixture := newLifecycleFixture(t)
	_, unit := startLifecycleImport(t, fixture, "", "nes")
	item, found, err := fixture.service.nextItem(fixture.context, unit)
	if err != nil || !found {
		t.Fatalf("next=%v error=%v", found, err)
	}
	if _, err := fixture.database.ExecContext(
		fixture.context,
		`UPDATE jobs SET worker_id='replacement',version=version+1 WHERE id=?`,
		unit.JobID,
	); err != nil {
		t.Fatal(err)
	}
	before := executionAuthorityState(t, fixture, unit)
	if err := fixture.service.processItem(fixture.context, unit, fixture.service.roots[unit.RootID], item); err == nil {
		t.Fatal("inactive execution process reported success")
	}
	if after := executionAuthorityState(t, fixture, unit); after != before {
		t.Fatalf("stale item worker changed execution: before=%s after=%s", before, after)
	}
}

func TestESImportCompletionRejectsLostExecutionAuthority(t *testing.T) {
	for _, mutation := range []string{"worker", "lease", "deadline"} {
		t.Run(mutation, func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			_, unit := startLifecycleImport(t, fixture, "", "nes")
			if _, err := fixture.database.ExecContext(
				fixture.context,
				`UPDATE emulationstation_import_items SET execution_state='BLOCKED_CONTENT',error_code='EMULATIONSTATION_CONTENT_FORMAT_UNSUPPORTED',completed_at_ms=?,version=version+1 WHERE import_id=?`,
				fixture.now.UnixMilli(),
				unit.ImportID,
			); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "worker":
				if _, err := fixture.database.ExecContext(
					fixture.context,
					`UPDATE jobs SET worker_id='replacement',version=version+1 WHERE id=?`,
					unit.JobID,
				); err != nil {
					t.Fatal(err)
				}
			case "lease":
				*fixture.now = fixture.now.Add(time.Minute)
			case "deadline":
				*fixture.now = fixture.now.Add(8 * 60 * time.Minute)
			}
			before := executionAuthorityState(t, fixture, unit)
			err := fixture.service.finishImport(fixture.context, unit)
			if after := executionAuthorityState(t, fixture, unit); after != before {
				t.Fatalf("unowned completion mutated durable execution: error=%v before=%s after=%s", err, before, after)
			}
			if err == nil {
				t.Fatal("unowned completion reported success")
			}
		})
	}
}
