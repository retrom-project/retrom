package emulationstationimport

import (
	"strings"
	"testing"
	"time"
)

func nextQueuedScan(t *testing.T) (lifecycleFixture, Summary) {
	t.Helper()
	fixture := newLifecycleFixture(t)
	created, err := fixture.service.Create(fixture.context, CreateRequest{RootID: "games"}, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, created
}

func TestNextESClaimRejectsExpiredQueuedDeadline(t *testing.T) {
	fixture, created := nextQueuedScan(t)
	now := fixture.now.UnixMilli()
	mustExecEmulationStationTest(t, fixture.database, `UPDATE jobs SET attempt_count=1,execution_started_at_ms=?,execution_deadline_at_ms=? WHERE id=?`, now-int64((8*time.Hour)/time.Millisecond), now, created.ScanJobID)
	unit, found, claimErr := fixture.service.claim(fixture.context)
	if claimErr != nil {
		t.Fatal(claimErr)
	}
	if found {
		t.Fatalf("claimed expired queued execution: %#v", unit)
	}
}

func TestNextESClaimRequiresExactLinkedJob(t *testing.T) {
	fixture, created := nextQueuedScan(t)
	mustExecEmulationStationTest(t, fixture.database, `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES('orphan-scan','EMULATIONSTATION_IMPORT',?,'SERVER_EMULATIONSTATION_SCAN',?,1,'{"inputExecutionNo":1}',1,'QUEUED',0,4,1,0,0,0)`, created.ID, strings.Repeat("c", 64))
	unit, found, claimErr := fixture.service.claim(fixture.context)
	if claimErr != nil {
		t.Fatal(claimErr)
	}
	if !found || unit.JobID != created.ScanJobID {
		t.Fatalf("claimed unlinked job: %#v found=%v linked=%s", unit, found, created.ScanJobID)
	}
}
