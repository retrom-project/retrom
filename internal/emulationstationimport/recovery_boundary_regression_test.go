package emulationstationimport

import (
	"testing"
	"time"
)

func TestNextESRecoveryDoesNotRewriteUnexpiredQueuedScan(t *testing.T) {
	fixture, created := nextQueuedScan(t)
	if err := fixture.service.recoverWork(fixture.context); err != nil {
		t.Fatal(err)
	}
	current, err := fixture.service.Get(fixture.context, created.ID)
	if err != nil || current.Version != created.Version {
		t.Fatalf("idle recovery rewrote fresh plan: before=%d after=%d error=%v", created.Version, current.Version, err)
	}
}

func TestNextESRecoveryTerminatesQueuedExhaustedBudget(t *testing.T) {
	fixture, created := nextQueuedScan(t)
	mustExecEmulationStationTest(t, fixture.database, `UPDATE jobs SET attempt_count=max_attempts,execution_started_at_ms=?,execution_deadline_at_ms=? WHERE id=?`, fixture.now.UnixMilli(), fixture.now.Add(time.Hour).UnixMilli(), created.ScanJobID)
	if err := fixture.service.recoverWork(fixture.context); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT state FROM jobs WHERE id=?`, created.ScanJobID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "FAILED" {
		t.Fatalf("queued exhausted job remained %s", state)
	}
}

func TestNextESRecoveryClearsInterruptedScanBeforeTerminalFailure(t *testing.T) {
	fixture, created := nextQueuedScan(t)
	unit, found, err := fixture.service.claim(fixture.context)
	if err != nil || !found {
		t.Fatalf("claim found=%v error=%v", found, err)
	}
	result, err := fixture.service.scan(fixture.context, fixture.service.roots[unit.RootID], unit.RelativePath, unit.ReleaseYearMax)
	if err != nil {
		t.Fatal(err)
	}
	now := fixture.now.UnixMilli()
	if len(result.Items) == 0 {
		t.Fatal("fixture has no partial scan items")
	}
	if err := fixture.service.persistScanHeaders(fixture.context, unit, result, now); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.persistScanItems(fixture.context, unit, result.Items, now); err != nil {
		t.Fatal(err)
	}
	mustExecEmulationStationTest(t, fixture.database, `UPDATE jobs SET leased_until_ms=?,execution_deadline_at_ms=? WHERE id=?`, now-1, now, created.ScanJobID)
	if err := fixture.service.recoverWork(fixture.context); err != nil {
		t.Fatalf("recover interrupted scan: %v", err)
	}
	var state string
	var items, events, payloads int
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT state,(SELECT count(*) FROM emulationstation_import_items WHERE import_id=?),(SELECT count(*) FROM job_events WHERE job_id=? AND event_type='FAILED'),(SELECT count(*) FROM jobs WHERE kind='PAYLOAD_RELEASE') FROM emulationstation_imports WHERE id=?`, created.ID, created.ScanJobID, created.ID).Scan(&state, &items, &events, &payloads); err != nil {
		t.Fatal(err)
	}
	if state != "FAILED" || items != 0 || events != 1 || payloads != 0 {
		t.Fatalf("recovered state=%s staged=%d events=%d payloads=%d", state, items, events, payloads)
	}
}
