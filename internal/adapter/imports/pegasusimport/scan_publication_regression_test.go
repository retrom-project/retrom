package pegasusimport

import (
	"strings"
	"testing"
	"time"

	pegasusimportmodel "retrom/internal/model/pegasusimport"
)

func scanPublicationFixture(t *testing.T) (*Service, work, scanResult) {
	t.Helper()
	db := newPegasusRetryDatabase(t)
	mustExecPegasusTest(t.Context(), t, db, `DELETE FROM pegasus_import_items;
UPDATE pegasus_imports SET state='SCANNING',phase='DISCOVERING_METADATA',import_job_id=NULL,
completed_at_ms=NULL,metadata_count=0,collection_count=0,game_count=0,mapped_collection_count=0,
processable_item_count=0,failed_item_count=0,retryable=0 WHERE id='import';
UPDATE jobs SET state='RUNNING',finished_at_ms=NULL,worker_id='scanner',leased_until_ms=90,
heartbeat_at_ms=2,execution_started_at_ms=2,execution_deadline_at_ms=100 WHERE id='scan';`)
	service := &Service{database: db, now: func() time.Time { return time.UnixMilli(10) }}
	unit := work{
		JobID: "scan", ImportID: "import", Kind: "SERVER_PEGASUS_SCAN",
		WorkerID: "scanner", ExecutionNo: 1, Attempt: 1,
	}
	digest := strings.Repeat("a", 64)
	result := scanResult{
		Metadata: []scannedMetadata{{Path: "metadata.pegasus.txt", Size: 1, Digest: digest, Facts: digest, State: "VALID"}},
		Items: []scannedItem{{
			ID: "scan-item", MetadataPath: "metadata.pegasus.txt", Title: "Game", SourceKey: digest,
			DiscoveryState: "READY", MetadataJSON: "{}", WarningsJSON: "[]",
			SourceManifestJSON: "{}", SourceManifestDigest: digest,
		}},
		SnapshotDigest: digest,
	}
	return service, unit, result
}

func TestScanPublicationRejectsReplacedOwnerAtEachStage(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"headers", "items", "finish"} {
		t.Run(stage, func(t *testing.T) {
			service, unit, result := scanPublicationFixture(t)
			if stage != "headers" {
				if err := service.persistScanHeaders(t.Context(), unit, result, 10); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "finish" {
				if err := service.persistScanItems(t.Context(), unit, result.Items, 10); err != nil {
					t.Fatal(err)
				}
			}
			mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET worker_id='replacement' WHERE id='scan'`)
			var err error
			switch stage {
			case "headers":
				err = service.persistScanHeaders(t.Context(), unit, result, 10)
			case "items":
				err = service.persistScanItems(t.Context(), unit, result.Items, 10)
			case "finish":
				err = service.finishScan(t.Context(), unit, result, 10)
			}
			if err == nil {
				t.Fatalf("replaced scan worker wrote %s", stage)
			}
		})
	}
}

func TestScanPublicationCannotFinishCancelledJob(t *testing.T) {
	t.Parallel()
	service, unit, result := scanPublicationFixture(t)
	if err := service.persistScanHeaders(t.Context(), unit, result, 10); err != nil {
		t.Fatal(err)
	}
	if err := service.persistScanItems(t.Context(), unit, result.Items, 10); err != nil {
		t.Fatal(err)
	}
	mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET state='CANCEL_REQUESTED',
cancel_reason='stop',cancel_requested_at_ms=10 WHERE id='scan';
UPDATE pegasus_imports SET state='CANCEL_REQUESTED',cancel_reason='stop' WHERE id='import'`)
	if err := service.finishScan(t.Context(), unit, result, 10); err == nil {
		t.Fatal("cancelled scan published success")
	}
}

func TestScanFailureClearsOnlyUnpublishedProjection(t *testing.T) {
	t.Parallel()
	service, unit, result := scanPublicationFixture(t)
	if err := service.persistScanHeaders(t.Context(), unit, result, 10); err != nil {
		t.Fatal(err)
	}
	if err := service.persistScanItems(t.Context(), unit, result.Items, 10); err != nil {
		t.Fatal(err)
	}
	if err := service.workerSettlement().Fail(t.Context(), unit.Identity(), pegasusimportmodel.ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true}); err != nil {
		t.Fatal(err)
	}
	assertEmptyScanProjection(t, service)
}

func TestRecoveredScanClearsPartialProjectionBeforeRetry(t *testing.T) {
	t.Parallel()
	service, unit, result := scanPublicationFixture(t)
	if err := service.persistScanHeaders(t.Context(), unit, result, 10); err != nil {
		t.Fatal(err)
	}
	if err := service.persistScanItems(t.Context(), unit, result.Items, 10); err != nil {
		t.Fatal(err)
	}
	mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET leased_until_ms=5 WHERE id='scan'`)
	if err := service.recoverWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertEmptyScanProjection(t, service)
	claimed, found, err := service.claim(t.Context())
	if err != nil || !found {
		t.Fatalf("reclaim scan: %v %v", found, err)
	}
	if err := service.persistScan(t.Context(), claimed, result); err != nil {
		t.Fatalf("publish recovered scan: %v", err)
	}
}

func assertEmptyScanProjection(t *testing.T, service *Service) {
	t.Helper()
	var count int
	err := service.database.QueryRowContext(t.Context(), `SELECT
(SELECT count(*) FROM pegasus_import_metadata_files WHERE import_id='import')+
(SELECT count(*) FROM pegasus_import_items WHERE import_id='import')`).Scan(&count)
	if err != nil || count != 0 {
		t.Fatalf("partial scan survived: count=%d err=%v", count, err)
	}
}
