package emulationstationimport

import (
	"errors"
	"testing"
	"time"

	"retrom/internal/capability/security/authn"
)

func TestPlanDeletionUpdatesTagVersionAndRetainsImmutableEvidence(t *testing.T) {
	fixture, mapped, tag := queryMappedTagFixture(t)
	if err := fixture.service.Delete(fixture.context, mapped.ID, mapped.Version); err != nil {
		t.Fatal(err)
	}
	after, err := fixture.service.tags.Get(fixture.context, tag.TagID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != tag.Version+1 || after.Usage.EmulationStationCollectionCount != 0 {
		t.Fatalf("deleted mapping left tag version=%d before=%d usage=%#v", after.Version, tag.Version, after.Usage)
	}
	var jobs, inputs, events int
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT (SELECT count(*) FROM jobs WHERE id=?),(SELECT count(*) FROM job_input_snapshots WHERE job_id=?),(SELECT count(*) FROM job_events WHERE job_id=?)`, mapped.ScanJobID, mapped.ScanJobID, mapped.ScanJobID).Scan(&jobs, &inputs, &events); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 || inputs != 1 || events == 0 {
		t.Fatalf("immutable evidence lost jobs=%d inputs=%d events=%d", jobs, inputs, events)
	}
}

func TestPlanDeletionRecordsActingUserAudit(t *testing.T) {
	fixture := newLifecycleFixture(t)
	scanned := fixture.createAndScan(t)
	if err := fixture.service.Delete(fixture.context, scanned.ID, scanned.Version); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT count(*) FROM audit_events WHERE action='EMULATIONSTATION_IMPORT_DELETED' AND resource_id=? AND actor_user_id=?`, scanned.ID, fixture.userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("deleted plan retained %d deletion audit records", count)
	}
}

func TestPlanDeletionPreservesQueryFailureCause(t *testing.T) {
	fixture := newLifecycleFixture(t)
	scanned := fixture.createAndScan(t)
	mustExecEmulationStationTest(t, fixture.database, `ALTER TABLE emulationstation_imports RENAME COLUMN state TO broken_state`)
	err := fixture.service.Delete(fixture.context, scanned.ID, scanned.Version)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("database query failure was mapped to missing plan: %v", err)
	}
}

func TestPlanExpiryCancelsBlockedDiscoveryWithoutDoubleCounting(t *testing.T) {
	fixture := newLifecycleFixture(t)
	writeScanFile(t, fixture.source, "blocked/gamelist.xml", []byte(`<gameList><game><path>missing.nes</path><name>Missing</name></game></gameList>`))
	scanned := fixture.createAndScan(t)
	if scanned.Counts.Blocked != 1 {
		t.Fatalf("fixture blocked count=%d", scanned.Counts.Blocked)
	}
	*fixture.now = fixture.now.Add(8 * 24 * time.Hour)
	if err := fixture.service.ExpirePlans(fixture.context); err != nil {
		t.Fatal(err)
	}
	expired, err := fixture.service.Get(fixture.context, scanned.ID)
	if err != nil {
		t.Fatal(err)
	}
	if expired.State != "EXPIRED" || expired.Counts.Blocked != 0 || expired.Counts.Cancelled != scanned.Counts.Games {
		t.Fatalf("expiry double counted discovery blockers: %#v", expired)
	}
	var releaseJobs int
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT count(*) FROM jobs WHERE kind='PAYLOAD_RELEASE'`).Scan(&releaseJobs); err != nil {
		t.Fatal(err)
	}
	if releaseJobs != 0 {
		t.Fatalf("unstarted expiry created %d release jobs", releaseJobs)
	}
}

func TestPlanDeletionAuditsCurrentPrincipalRatherThanCreator(t *testing.T) {
	fixture, mapped, tag := queryMappedTagFixture(t)
	const actor = "019b0000-0000-7000-8000-000000000089"
	mustExecEmulationStationTest(t, fixture.database, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('deleting-admin-profile','Admin',1)`)
	mustExecEmulationStationTest(t, fixture.database, `INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms) VALUES(?,'deleting-admin-profile','deleting-admin','Admin','ADMIN','ENABLED',1,1)`, actor)
	ctx := authn.WithPrincipal(fixture.context, authn.Principal{UserID: actor, ProfileID: "deleting-admin-profile", Role: "ADMIN"})
	if err := fixture.service.Delete(ctx, mapped.ID, mapped.Version); err != nil {
		t.Fatal(err)
	}
	var auditActor, tagActor string
	if err := fixture.database.QueryRowContext(ctx, `SELECT actor_user_id FROM audit_events WHERE action='EMULATIONSTATION_IMPORT_DELETED' AND resource_id=?`, mapped.ID).Scan(&auditActor); err != nil {
		t.Fatal(err)
	}
	if err := fixture.database.QueryRowContext(ctx, `SELECT updated_by_user_id FROM tags WHERE id=?`, tag.TagID).Scan(&tagActor); err != nil {
		t.Fatal(err)
	}
	if auditActor != actor || tagActor != actor {
		t.Fatalf("deletion actor audit=%s tag=%s", auditActor, tagActor)
	}
}
