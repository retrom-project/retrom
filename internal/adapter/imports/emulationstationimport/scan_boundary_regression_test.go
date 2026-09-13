package emulationstationimport

import (
	"errors"
	"strconv"
	"testing"
	"time"

	jobrepository "retrom/internal/repo/jobs"
	jobservice "retrom/internal/service/jobs"

	"github.com/google/uuid"
)

func nextScannedOwnedResult(t *testing.T) (lifecycleFixture, Summary, work, scanResult) {
	t.Helper()
	fixture, created := nextQueuedScan(t)
	unit, found, claimErr := fixture.service.claim(fixture.context)
	if claimErr != nil {
		t.Fatal(claimErr)
	}
	if !found {
		t.Fatal("scan was not claimed")
	}
	result, err := fixture.service.scan(fixture.context, fixture.service.roots[unit.RootID], unit.RelativePath, unit.ReleaseYearMax)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, created, unit, result
}

func TestNextESScanHeaderRejectsReplacedAttempt(t *testing.T) {
	fixture, _, unit, result := nextScannedOwnedResult(t)
	mustExecEmulationStationTest(t, fixture.database, `UPDATE jobs SET attempt_count=attempt_count+1,worker_id='replacement-worker',version=version+1 WHERE id=?`, unit.JobID)
	err := fixture.service.persistScanHeaders(fixture.context, unit, result)
	if err == nil {
		t.Fatal("stale attempt wrote scan headers")
	}
}

func TestNextESCancelledScanCannotPublishSuccess(t *testing.T) {
	fixture, created, unit, result := nextScannedOwnedResult(t)
	if err := fixture.service.persistScanHeaders(fixture.context, unit, result); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.persistScanItems(fixture.context, unit, result.Items); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT version FROM jobs WHERE id=?`, unit.JobID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if _, pending, err := jobservice.New(jobrepository.New(fixture.database), func() time.Time { return *fixture.now }).Cancel(fixture.context, unit.JobID, version, "Stop"); err != nil || !pending {
		t.Fatalf("generic cancel pending=%v error=%v", pending, err)
	}
	err := fixture.service.finishScan(fixture.context, unit, result)
	var state string
	var success int
	if queryErr := fixture.database.QueryRowContext(fixture.context, `SELECT state,(SELECT count(*) FROM job_events WHERE job_id=? AND event_type='SUCCEEDED') FROM emulationstation_imports WHERE id=?`, unit.JobID, created.ID).Scan(&state, &success); queryErr != nil {
		t.Fatal(queryErr)
	}
	if err == nil || state != "SCANNING" || success != 0 {
		t.Fatalf("cancelled scan published state=%s succeeded=%d error=%v", state, success, err)
	}
}

type scanEntropyReader struct{ remaining int }

func (reader *scanEntropyReader) Read(value []byte) (int, error) {
	if reader.remaining == 0 {
		return 0, errCreationEntropy
	}
	reader.remaining--
	for i := range value {
		value[i] = byte(i + reader.remaining)
	}
	return len(value), nil
}

func TestNextESScanChecksEveryGeneratedIdentity(t *testing.T) {
	for _, allowed := range []int{0, 1} {
		t.Run(strconv.Itoa(allowed), func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			result, err := func() (scanResult, error) {
				uuid.SetRand(&scanEntropyReader{remaining: allowed})
				defer uuid.SetRand(nil)
				return fixture.service.scan(fixture.context, fixture.service.roots["games"], "", 2027)
			}()
			if !errors.Is(err, errCreationEntropy) || len(result.Collections) != 0 || len(result.Items) != 0 {
				t.Fatalf("allowed=%d scan collections=%d items=%d error=%v", allowed, len(result.Collections), len(result.Items), err)
			}
		})
	}
}

func TestNextESScanExecutionCannotPublishReplacementAttempt(t *testing.T) {
	fixture, created := nextQueuedScan(t)
	unit, found, err := fixture.service.claim(fixture.context)
	if err != nil || !found {
		t.Fatalf("claim=%v %v", found, err)
	}
	mustExecEmulationStationTest(t, fixture.database, `UPDATE jobs SET attempt_count=attempt_count+1,worker_id='replacement-worker',version=version+1 WHERE id=?`, unit.JobID)
	fixture.service.executeScan(fixture.context, unit, fixture.service.roots[unit.RootID])
	var state, job string
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT state,(SELECT state FROM jobs WHERE id=?) FROM emulationstation_imports WHERE id=?`, unit.JobID, created.ID).Scan(&state, &job); err != nil {
		t.Fatal(err)
	}
	if state != "SCANNING" || job != "RUNNING" {
		t.Fatalf("old attempt changed replacement to plan=%s job=%s", state, job)
	}
}
