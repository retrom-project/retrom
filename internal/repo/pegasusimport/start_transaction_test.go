package pegasusimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	pegasusimportmodel "retrom/internal/model/pegasusimport"
	tagrepository "retrom/internal/repo/tagging"
	application "retrom/internal/service/pegasusimport"
	"retrom/internal/service/tagging"
)

const skippedStartCollection = "019b0000-0000-7000-8000-000000000004"

func startDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := mappingDatabase(t)
	instance := seedMappingTarget(t, db)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO pegasus_import_collections(id,import_id,metadata_relative_path,segment_ordinal,name,game_count,created_at_ms,updated_at_ms)
VALUES(?,'import-0','metadata.pegasus.txt',1,'Skipped',1,1,1)`, skippedStartCollection); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE pegasus_imports SET collection_count=2,game_count=3,
source_snapshot_digest='cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc';
INSERT INTO pegasus_import_metadata_files(import_id,relative_path,size_bytes,content_digest,source_facts_digest,parse_state,created_at_ms)
VALUES('import-0','metadata.pegasus.txt',10,'dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',
'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee','VALID',1);`); err != nil {
		t.Fatal(err)
	}
	mapper := application.NewMappings(NewMappings(db), tagging.New(tagrepository.New(db), time.Now), func() time.Time { return time.UnixMilli(2) })
	if _, err := mapper.Update(t.Context(), "import-0", 1, []pegasusimportmodel.Mapping{
		{CollectionID: mappingCollection, Action: "IMPORT", PlatformInstanceID: instance, TagIDs: []string{mappingTag}},
		{CollectionID: skippedStartCollection, Action: "SKIP", TagIDs: []string{}},
	}, mappingActor); err != nil {
		t.Fatal(err)
	}
	for i, collection := range []string{mappingCollection, skippedStartCollection, mappingCollection} {
		discovery := "READY"
		if i == 0 {
			discovery = "BLOCKED_SOURCE"
		}
		if _, err := db.ExecContext(t.Context(), `INSERT INTO pegasus_import_items(id,import_id,collection_id,metadata_relative_path,game_ordinal,source_key,title,
discovery_state,execution_state,metadata_json,source_manifest_json,source_manifest_digest,created_at_ms,updated_at_ms)
VALUES(?,'import-0',?,'metadata.pegasus.txt',?,?,'Item',?,'PENDING','{}','{}',
'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',1,1)`, fmt.Sprintf("start-item-%d", i), collection, i, fmt.Sprintf("%064x", i), discovery); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestStartRollsBackJobInputItemsAndReleasesOnFailure(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"job", "version", "root", "expiry", "audit", "callback"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			db := startDatabase(t)
			beforeRows := workflowRows(t, db)
			cause := errors.New("late start failure")
			err := NewStarter(db).WithStart(t.Context(), func(scope pegasusimportmodel.StartScope) error {
				before, err := scope.Read.Current(t.Context(), "import-0")
				if err != nil {
					return err
				}
				plan := pegasusimportmodel.StartPlan{Before: before, JobID: "start-job", ExecutionID: "start-execution", AuditID: "start-audit", ActorID: mappingActor, DedupeKey: strings.Repeat("1", 64), NowMS: 10}
				invalidateStartPlan(&plan, failure)
				if err := scope.Write.Queue(t.Context(), plan); err != nil {
					return err
				}
				return cause
			})
			if err == nil {
				t.Fatal("invalid start committed")
			}
			if failure == "callback" && !errors.Is(err, cause) {
				t.Fatalf("lost callback failure: %v", err)
			}
			if !reflect.DeepEqual(workflowRows(t, db), beforeRows) {
				t.Fatal("failed start left job, item, evidence or release writes")
			}
		})
	}
}

func invalidateStartPlan(plan *pegasusimportmodel.StartPlan, failure string) {
	switch failure {
	case "job":
		plan.JobID = "job-0"
	case "version":
		plan.Before.Summary.Version++
	case "root":
		plan.Before.RootConfigDigest = "changed"
	case "expiry":
		plan.NowMS = plan.Before.Summary.ExpiresAtMS
	case "audit":
		plan.AuditID = "audit-0"
	}
}

type verifiedStartSource struct{ database *sql.DB }

func (source verifiedStartSource) Select(_ context.Context, id, _ string) (pegasusimportmodel.SelectedRoot, error) {
	return pegasusimportmodel.SelectedRoot{ID: id, Digest: strings.Repeat("a", 64)}, nil
}

func (source verifiedStartSource) VerifyMetadata(ctx context.Context, _, _ string, metadata []pegasusimportmodel.MetadataEvidence) error {
	if source.database.Stats().InUse != 0 {
		return errors.New("verification retained database connection")
	}
	if len(metadata) != 1 || metadata[0].RelativePath != "metadata.pegasus.txt" {
		return errors.New("metadata evidence missing")
	}
	var value int
	if err := source.database.QueryRowContext(ctx, `SELECT 1`).Scan(&value); err != nil {
		return fmt.Errorf("verification database availability: %w", err)
	}
	return nil
}

func TestStartQueuesFrozenInputAndTerminalPayloadsExactlyOnce(t *testing.T) {
	t.Parallel()
	db := startDatabase(t)
	service := application.NewStarter(NewStarter(db), verifiedStartSource{database: db}, func() time.Time { return time.UnixMilli(10) })
	result, queued, err := service.Start(t.Context(), "import-0", 2, "actor")
	if err != nil {
		t.Fatal(err)
	}
	if !queued || result.State != "QUEUED" || result.Version != 3 || result.Counts.Blocked != 1 || result.ImportJobID == nil {
		t.Fatalf("queued start: %#v %v", result, queued)
	}
	assertStartItemsAndEvidence(t, db, *result.ImportJobID)
	before := workflowRows(t, db)
	repeated, queued, err := service.Start(t.Context(), "import-0", 2, "actor")
	if err != nil || queued || repeated.Version != result.Version {
		t.Fatalf("repeated start: %#v queued=%v %v", repeated, queued, err)
	}
	if !reflect.DeepEqual(workflowRows(t, db), before) {
		t.Fatal("repeated start created execution or release evidence")
	}
}

func assertStartItemsAndEvidence(t *testing.T, db *sql.DB, jobID string) {
	t.Helper()
	for i, want := range []string{"BLOCKED_SOURCE", "SKIPPED_MAPPING", "PENDING"} {
		var state, payload string
		if err := db.QueryRowContext(t.Context(), `SELECT execution_state,payload_state FROM pegasus_import_items WHERE id=?`, fmt.Sprintf("start-item-%d", i)).Scan(&state, &payload); err != nil {
			t.Fatal(err)
		}
		wantPayload := "RELEASING"
		if i == 2 {
			wantPayload = "RETAINED"
		}
		if state != want || payload != wantPayload {
			t.Fatalf("start item %d: %s %s", i, state, payload)
		}
	}
	var root, actor string
	var version int
	var releases int
	if err := db.QueryRowContext(t.Context(), `SELECT json_extract(input_json,'$.inputs.rootConfigDigest'),
json_extract(input_json,'$.inputs.sourceSnapshotVersion'),
(SELECT actor_user_id FROM audit_events WHERE action='PEGASUS_IMPORT_STARTED'),
(SELECT count(*) FROM jobs WHERE kind='PAYLOAD_RELEASE')
FROM job_input_snapshots WHERE job_id=? AND execution_no=1`, jobID).Scan(&root, &version, &actor, &releases); err != nil {
		t.Fatal(err)
	}
	if root != strings.Repeat("a", 64) || version != 2 || actor != "actor" || releases != 2 {
		t.Fatalf("start evidence: root=%s version=%d actor=%s releases=%d", root, version, actor, releases)
	}
}
