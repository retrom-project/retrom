package pegasusimport

import (
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	pegasusimportmodel "retrom/internal/model/pegasusimport"
	pegasusimportservice "retrom/internal/service/pegasusimport"
)

func publicationDatabase(t *testing.T) (*sql.DB, pegasusimportmodel.ExecutionIdentity, pegasusimportmodel.ScanProjection) {
	t.Helper()
	db := creationDatabase(t)
	if err := NewCreation(db).WithCreate(t.Context(), func(writer pegasusimportmodel.CreationWriter) error {
		_, err := writer.Insert(t.Context(), creationPlan(0))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(
		t.Context(),
		`UPDATE jobs SET state='RUNNING',attempt_count=1,worker_id='scanner',
leased_until_ms=90,heartbeat_at_ms=2,execution_started_at_ms=2,execution_deadline_at_ms=100 WHERE id='job-0'`,
	); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	id := pegasusimportmodel.ExecutionIdentity{
		JobID:       "job-0",
		ImportID:    "import-0",
		WorkerID:    "scanner",
		ExecutionNo: 1,
		Attempt:     1,
	}
	projection := pegasusimportmodel.ScanProjection{
		Headers: pegasusimportmodel.ScanHeaders{Metadata: []pegasusimportmodel.ScanMetadata{
			{Path: "metadata.pegasus.txt", Size: 1, Digest: digest, Facts: digest, State: "VALID"},
		}},
		Items: []pegasusimportmodel.ScanItem{{
			ID: "scanned", MetadataPath: "metadata.pegasus.txt", SourceKey: digest, Title: "Game",
			DiscoveryState: "READY", MetadataJSON: "{}", WarningsJSON: "[]", SourceManifestJSON: "{}", SourceManifestDigest: digest,
			Files: []pegasusimportmodel.ScanFile{{Ordinal: 0, Kind: "FILE", Path: "game.gba", Size: 1, Facts: digest}},
		}},

		Summary: pegasusimportmodel.ScanSummary{SnapshotDigest: digest, Shape: pegasusimportmodel.ScanShape{Metadata: 1, Items: 1, EstimatedBytes: 1}},
	}
	return db, id, projection
}

func publicationRows(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	result := workflowRows(t, db)
	for _, table := range []string{
		"pegasus_import_metadata_files", "pegasus_import_collections",
		"pegasus_import_item_files", "pegasus_import_item_assets", "pegasus_collection_tags",
	} {
		result[table] = workflowTable(t, db, table)
	}
	return result
}

func TestScanPublicationRepositoryFencesEveryStage(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"headers", "items", "finish"} {
		t.Run(stage, func(t *testing.T) {
			for _, field := range []string{
				"job version",
				"parent version",
				"execution",
				"attempt",
				"worker",
				"lease",
				"deadline",
				"parent state",
			} {
				t.Run(field, func(t *testing.T) { assertScanPublicationFence(t, stage, field) })
			}
		})
	}
}

func assertScanPublicationFence(t *testing.T, stage, field string) {
	t.Helper()
	db, id, projection := publicationDatabase(t)
	before := publicationRows(t, db)
	err := NewScanPublication(db).WithScan(t.Context(), func(scope pegasusimportmodel.ScanScope) error {
		current, err := scope.Read.Current(t.Context(), id.JobID)
		if err != nil {
			return err
		}
		invalidateRecovery(&current, field)
		owner := pegasusimportmodel.ScanLease{Before: current, NowMS: 10}
		switch stage {
		case "headers":
			return scope.Write.Headers(t.Context(), owner, projection.Headers)
		case "items":
			return scope.Write.Items(t.Context(), owner, projection.Items)
		default:
			return scope.Write.Finish(t.Context(), owner, projection.Summary)
		}
	})
	if !errors.Is(err, pegasusimportmodel.ErrVersionConflict) {
		t.Fatalf("%s %s: %v", stage, field, err)
	}
	if !reflect.DeepEqual(before, publicationRows(t, db)) {
		t.Fatal("rejected scanner changed persisted rows")
	}
}

func stagePublication(
	t *testing.T,
	db *sql.DB,
	id pegasusimportmodel.ExecutionIdentity,
	projection pegasusimportmodel.ScanProjection,
) {
	t.Helper()
	service := pegasusimportservice.NewScanPublication(NewScanPublication(db), func() time.Time { return time.UnixMilli(10) })
	if err := service.Headers(t.Context(), id, projection.Headers); err != nil {
		t.Fatal(err)
	}
	if err := service.Items(t.Context(), id, projection.Items); err != nil {
		t.Fatal(err)
	}
}

func TestScanPublicationLateFailureRollsBackStateEventAndProjection(t *testing.T) {
	t.Parallel()
	db, id, projection := publicationDatabase(t)
	stagePublication(t, db, id, projection)
	before := publicationRows(t, db)
	cause := errors.New("abort after actual publication")
	err := NewScanPublication(db).WithScan(t.Context(), func(scope pegasusimportmodel.ScanScope) error {
		current, err := scope.Read.Current(t.Context(), id.JobID)
		if err != nil {
			return err
		}
		if err := scope.Write.Finish(
			t.Context(),
			pegasusimportmodel.ScanLease{Before: current, NowMS: 10},
			projection.Summary,
		); err != nil {
			return err
		}
		after, err := scope.Read.Current(t.Context(), id.JobID)
		if err != nil {
			return err
		}
		if after.JobState != "SUCCEEDED" || after.ImportState != "AWAITING_MAPPING" {
			t.Fatalf("publication did not run: %#v", after)
		}
		return cause
	})
	if !errors.Is(err, cause) || !reflect.DeepEqual(before, publicationRows(t, db)) {
		t.Fatalf("partial publication: %v", err)
	}
}
