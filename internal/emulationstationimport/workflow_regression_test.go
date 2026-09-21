package emulationstationimport

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func workflowRetryFixture(t *testing.T) (lifecycleFixture, Summary) {
	t.Helper()
	fixture := newLifecycleFixture(t)
	started, unit := startLifecycleImport(t, fixture, "", "nes")
	return fixture, markClaimedImportRetryable(t, fixture, started, unit)
}

func workflowCancelFixture(t *testing.T) (lifecycleFixture, Summary) {
	t.Helper()
	fixture, mapped, _ := queryMappedTagFixture(t)
	queued, err := fixture.service.StartImport(fixture.context, mapped.ID, mapped.Version)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, queued
}

func TestWorkflowCancelPreservesStorageCause(t *testing.T) {
	fixture, queued := workflowCancelFixture(t)
	mustExecEmulationStationTest(t, fixture.database, `ALTER TABLE emulationstation_imports RENAME COLUMN import_job_id TO broken_import_job_id`)
	result, pending, err := fixture.service.Cancel(fixture.context, queued.ID, queued.Version, "Stop", fixture.userID)
	if result.ID != "" || pending || err == nil || errors.Is(err, ErrNotCancellable) || !strings.Contains(err.Error(), "import_job_id") {
		t.Fatalf("cancel storage failure result=%#v pending=%v error=%v", result, pending, err)
	}
}

func TestWorkflowCancelRejectsUnavailableAuditIdentity(t *testing.T) {
	fixture, queued := workflowCancelFixture(t)
	result, pending, err := func() (Summary, bool, error) {
		uuid.SetRand(creationEntropyFailure{})
		defer uuid.SetRand(nil)
		return fixture.service.Cancel(fixture.context, queued.ID, queued.Version, "Stop", fixture.userID)
	}()
	if result.ID != "" || pending || !errors.Is(err, errCreationEntropy) {
		t.Fatalf("cancel identity failure result=%#v pending=%v error=%v", result, pending, err)
	}
}

func TestWorkflowRetryPreservesStorageCause(t *testing.T) {
	for _, field := range []string{"root_label_snapshot", "root_config_digest"} {
		t.Run(field, func(t *testing.T) {
			fixture, failed := workflowRetryFixture(t)
			mustExecEmulationStationTest(t, fixture.database, `ALTER TABLE emulationstation_imports RENAME COLUMN `+field+` TO broken_snapshot_field`)
			result, err := fixture.service.Retry(fixture.context, failed.ID, failed.Version, fixture.userID)
			if result.ID != "" || err == nil || errors.Is(err, ErrNotRetryable) || errors.Is(err, ErrSourceChanged) || !strings.Contains(err.Error(), field) {
				t.Fatalf("retry storage failure result=%#v error=%v", result, err)
			}
		})
	}
}

func TestWorkflowRetryRejectsUnavailableExecutionIdentity(t *testing.T) {
	fixture, failed := workflowRetryFixture(t)
	result, err := func() (Summary, error) {
		uuid.SetRand(creationEntropyFailure{})
		defer uuid.SetRand(nil)
		return fixture.service.Retry(fixture.context, failed.ID, failed.Version, fixture.userID)
	}()
	if result.ID != "" || !errors.Is(err, errCreationEntropy) {
		t.Fatalf("retry identity failure result=%#v error=%v", result, err)
	}
}

func TestWorkflowRetryRefreshesFailureCounts(t *testing.T) {
	fixture, failed := workflowRetryFixture(t)
	result, err := fixture.service.Retry(fixture.context, failed.ID, failed.Version, fixture.userID)
	if err != nil || result.State != "QUEUED" || result.Counts.Failed != 0 {
		t.Fatalf("retry kept reset failures result=%#v error=%v", result, err)
	}
}

func TestWorkflowRetryRecordsActingUserAudit(t *testing.T) {
	fixture, failed := workflowRetryFixture(t)
	const actor = "01980000-0000-7000-8000-000000000845"
	mustExecEmulationStationTest(t, fixture.database, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('workflow-actor-profile','Workflow actor',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'workflow-actor-profile','workflow-actor','Workflow actor','ADMIN','ENABLED',1,1)`, actor)
	if _, err := fixture.service.Retry(fixture.context, failed.ID, failed.Version, actor); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT count(*) FROM audit_events WHERE resource_id=? AND action='EMULATIONSTATION_IMPORT_RETRIED' AND actor_user_id=?`, failed.ID, actor).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("retry has %d acting-user audit records", count)
	}
}

func TestWorkflowRetryRejectsIneligibleFrozenTargets(t *testing.T) {
	cases := []struct{ name, query string }{
		{"platform", `UPDATE platforms SET enabled=0 WHERE id=(SELECT target_platform_id FROM emulationstation_import_collections WHERE import_id=? LIMIT 1)`},
		{"core", `UPDATE cores SET enabled=0 WHERE id=(SELECT target_default_core_id FROM emulationstation_import_collections WHERE import_id=? LIMIT 1)`},
		{"membership", `DELETE FROM runtime_binding_platforms WHERE platform_id=(SELECT target_platform_id FROM emulationstation_import_collections WHERE import_id=? LIMIT 1)`},
		{"binding", `DELETE FROM runtime_target_bindings WHERE core_id=(SELECT target_default_core_id FROM emulationstation_import_collections WHERE import_id=? LIMIT 1)`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture, failed := workflowRetryFixture(t)
			mustExecEmulationStationTest(t, fixture.database, test.query, failed.ID)
			result, err := fixture.service.Retry(fixture.context, failed.ID, failed.Version, fixture.userID)
			if result.ID != "" || !errors.Is(err, ErrMappingTargetChanged) {
				t.Fatalf("%s target accepted result=%#v error=%v", test.name, result, err)
			}
		})
	}
}
