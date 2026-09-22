package sourceimport

import (
	"errors"
	"fmt"
	"testing"
)

func TestScanCancellationClosesQueuedAndRequestsRunningExecution(t *testing.T) {
	t.Parallel()
	for _, queued := range []bool{false, true} {
		t.Run(fmt.Sprintf("queued=%v", queued), func(t *testing.T) {
			service, unit, result := scanPublicationFixture(t)
			if err := service.persistScanHeaders(t.Context(), unit, result, 10); err != nil {
				t.Fatal(err)
			}
			if queued {
				mustExecSourceTest(t.Context(), t, service.database, `UPDATE jobs SET state='QUEUED',worker_id=NULL,
leased_until_ms=NULL,heartbeat_at_ms=NULL WHERE id='scan'`)
			}
			before, err := service.Get(t.Context(), unit.ImportID)
			if err != nil {
				t.Fatal(err)
			}
			after, pending, err := service.Cancel(t.Context(), unit.ImportID, before.Version, "Stop scan", "user")
			if err != nil || pending == queued {
				t.Fatalf("queued=%v scan cancellation pending=%v err=%v", queued, pending, err)
			}
			expected := "CANCEL_REQUESTED"
			if queued {
				expected = "CANCELLED"
			}
			if after.State != expected || after.Version != before.Version+1 {
				t.Fatalf("scan cancel result=%#v", after)
			}
			if !queued {
				settleRunningScanCancellation(t, service, unit, result)
			}
			assertEmptyScanProjection(t, service)
		})
	}
}

func TestScanCancellationCanCoexistWithAnotherExecutingImport(t *testing.T) {
	t.Parallel()
	service, unit, _ := scanPublicationFixture(t)
	mustExecSourceTest(t.Context(), t, service.database, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
VALUES('other-scan','SOURCE_IMPORT','other','IMPORT_SCAN',
'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',1,'{}',1,'QUEUED',0,4,1,1,1);
UPDATE jobs SET scope_id='other',state='RUNNING',finished_at_ms=NULL WHERE id='work';
INSERT INTO source_imports(id,root_id,root_label_snapshot,source_relative_path,root_config_digest,
state,scan_job_id,import_job_id,created_by_user_id,created_at_ms,updated_at_ms,expires_at_ms)
VALUES('other','games','Games','',
'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','RUNNING','other-scan','work','user',1,1,100);`)
	before, err := service.Get(t.Context(), unit.ImportID)
	if err != nil {
		t.Fatal(err)
	}
	after, pending, err := service.Cancel(t.Context(), unit.ImportID, before.Version, "Stop scan", "user")
	if err != nil || !pending || after.State != "CANCEL_REQUESTED" {
		t.Fatalf("independent scan cancellation collided with import: pending=%v err=%v", pending, err)
	}
}

func settleRunningScanCancellation(t *testing.T, service *Service, unit work, result scanResult) {
	t.Helper()
	if err := service.finishScan(t.Context(), unit, result, 10); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("cancelled scan published: %v", err)
	}
	if closed, err := service.closeCancelled(t.Context(), unit); !closed || err != nil {
		t.Fatalf("scan cancellation settlement: %v %v", closed, err)
	}
}
