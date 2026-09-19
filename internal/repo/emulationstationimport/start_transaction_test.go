package emulationstationimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	tagrepository "retrom/internal/repo/tagging"
	application "retrom/internal/service/emulationstationimport"
	"retrom/internal/service/tagging"
)

func startDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := mappingDatabase(t)
	instance := seedMappingTarget(t, db)
	seedSecondMappingCollection(t, db)
	if _, err := db.ExecContext(t.Context(), `UPDATE emulationstation_imports SET game_count=3,processable_item_count=2,blocked_item_count=1;
UPDATE emulationstation_import_collections SET game_count=2 WHERE id='`+mappingCollection+`';
UPDATE emulationstation_import_gamelists SET game_count=2 WHERE relative_path='gamelist.xml';
INSERT INTO content_kinds(id) VALUES('SINGLE_FILE') ON CONFLICT(id) DO NOTHING;`); err != nil {
		t.Fatal(err)
	}
	mapper := application.NewMappings(NewMappings(db), tagging.New(tagrepository.New(db), time.Now), func() time.Time { return time.UnixMilli(2) })
	if _, err := mapper.Update(t.Context(), "import-0", 1, []emulationstationimportmodel.Mapping{
		{CollectionID: mappingCollection, Action: "IMPORT", PlatformInstanceID: instance, TagIDs: []string{mappingTag}},
		{CollectionID: secondMappingCollection, Action: "SKIP", TagIDs: []string{}},
	}, mappingActor); err != nil {
		t.Fatal(err)
	}
	for i, collection := range []string{mappingCollection, secondMappingCollection, mappingCollection} {
		discovery, path := "READY", "gamelist.xml"
		if i == 0 {
			discovery = "BLOCKED_SOURCE"
		}
		if i == 1 {
			path = "nested/gamelist.xml"
		}
		if _, err := db.ExecContext(t.Context(), `INSERT INTO emulationstation_import_items(id,import_id,collection_id,gamelist_relative_path,game_ordinal,source_key,title,source_flags_json,discovery_state,execution_state,content_kind,metadata_json,source_manifest_json,source_manifest_digest,created_at_ms,updated_at_ms)
VALUES(?,'import-0',?,?,?,?,'Item','{"hidden":false,"adult":false,"kidGame":false}',?,'PENDING','SINGLE_FILE',
'{"schemaVersion":1,"title":"Item","description":"","developer":"","publisher":"","genre":"","players":null,"releaseYear":null}',
'{"schemaVersion":1,"contentKind":"SINGLE_FILE","files":[]}',?,1,1)`, fmt.Sprintf("start-item-%d", i), collection, path, i+1, fmt.Sprintf("%064x", i), discovery, planDigest); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestStartRollsBackJobInputItemsAndReleasesOnFailure(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"job", "version", "mapping version", "root", "source", "year", "expiry", "audit", "callback"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			db := startDatabase(t)
			beforeRows := planRows(t, db)
			cause := errors.New("late start failure")
			err := NewStarter(db).WithStart(t.Context(), func(scope emulationstationimportmodel.StartScope) error {
				before, err := scope.Read.Current(t.Context(), "import-0")
				if err != nil {
					return err
				}
				plan := emulationstationimportmodel.StartPlan{Before: before, JobID: "start-job", ExecutionID: "start-execution", AuditID: "start-audit", ActorID: mappingActor, DedupeKey: strings.Repeat("1", 64), NowMS: 10}
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
			if !reflect.DeepEqual(planRows(t, db), beforeRows) {
				t.Fatal("failed start left job, item, evidence or release writes")
			}
		})
	}
}

func invalidateStartPlan(plan *emulationstationimportmodel.StartPlan, failure string) {
	switch failure {
	case "job":
		plan.JobID = "job-0"
	case "version":
		plan.Before.Summary.Version++
	case "mapping version":
		plan.Before.Summary.MappingVersion++
	case "root":
		plan.Before.RootConfigDigest = "changed"
	case "source":
		plan.Before.SourceSnapshotDigest = "changed"
	case "year":
		plan.Before.ReleaseYearMax++
	case "expiry":
		plan.NowMS = plan.Before.Summary.ExpiresAtMS
	case "audit":
		plan.AuditID = "audit-0"
	}
}

type verifiedStartSource struct{ database *sql.DB }

func (source verifiedStartSource) Select(_ context.Context, id, _ string) (emulationstationimportmodel.SelectedRoot, error) {
	return emulationstationimportmodel.SelectedRoot{ID: id, Digest: strings.Repeat("a", 64)}, nil
}

func (source verifiedStartSource) VerifyGamelists(ctx context.Context, _, _ string, metadata []emulationstationimportmodel.GamelistEvidence) error {
	if source.database.Stats().InUse != 0 {
		return errors.New("verification retained database connection")
	}
	if len(metadata) != 2 || metadata[0].RelativePath != "gamelist.xml" {
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
	tagsBefore := planTable(t, db, "tags")
	service := application.NewStarter(NewStarter(db), verifiedStartSource{database: db}, func() time.Time { return time.UnixMilli(10) })
	result, queued, err := service.Start(t.Context(), "import-0", 2, "actor")
	if err != nil {
		t.Fatal(err)
	}
	if !queued || result.State != "QUEUED" || result.Version != 3 || result.Counts.Blocked != 1 || result.ImportJobID == nil {
		t.Fatalf("queued start: %#v %v", result, queued)
	}
	assertStartItemsAndEvidence(t, db, *result.ImportJobID)
	assertStartFrozenInput(t, db, *result.ImportJobID)
	if planTable(t, db, "tags") != tagsBefore {
		t.Fatal("start changed assigned tags or tag versions")
	}
	before := planRows(t, db)
	repeated, queued, err := service.Start(t.Context(), "import-0", result.Version, "actor")
	if err != nil || queued || repeated.Version != result.Version {
		t.Fatalf("repeated start: %#v queued=%v %v", repeated, queued, err)
	}
	if !reflect.DeepEqual(planRows(t, db), before) {
		t.Fatal("repeated start created execution or release evidence")
	}
}

func assertStartItemsAndEvidence(t *testing.T, db *sql.DB, jobID string) {
	t.Helper()
	for i, want := range []string{"BLOCKED_SOURCE", "SKIPPED_MAPPING", "PENDING"} {
		var state, payload string
		if err := db.QueryRowContext(t.Context(), `SELECT execution_state,payload_state FROM emulationstation_import_items WHERE id=?`, fmt.Sprintf("start-item-%d", i)).Scan(&state, &payload); err != nil {
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
(SELECT actor_user_id FROM audit_events WHERE action='EMULATIONSTATION_IMPORT_STARTED'),
(SELECT count(*) FROM jobs WHERE kind='PAYLOAD_RELEASE')
FROM job_input_snapshots WHERE job_id=? AND execution_no=1`, jobID).Scan(&root, &version, &actor, &releases); err != nil {
		t.Fatal(err)
	}
	if root != strings.Repeat("a", 64) || version != 2 || actor != "actor" || releases != 2 {
		t.Fatalf("start evidence: root=%s version=%d actor=%s releases=%d", root, version, actor, releases)
	}
}

func assertStartFrozenInput(t *testing.T, db *sql.DB, jobID string) {
	t.Helper()
	var encoded, storedDigest, dedupe, kind, scope string
	var year, tagVersion int
	if err := db.QueryRowContext(t.Context(), `SELECT input.input_json,input.input_digest,job.dedupe_key,job.kind,job.scope_type,
(SELECT release_year_max FROM emulationstation_imports WHERE id='import-0'),
(SELECT version FROM tags WHERE id=?) FROM job_input_snapshots input JOIN jobs job ON job.id=input.job_id
WHERE job.id=?`, mappingTag, jobID).Scan(&encoded, &storedDigest, &dedupe, &kind, &scope, &year, &tagVersion); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(encoded))
	wantDedupe := sha256.Sum256([]byte("retrom-job-dedupe-v1\x00SERVER_EMULATIONSTATION_IMPORT\x00import-0"))
	var input struct {
		SchemaVersion int                        `json:"schemaVersion"`
		ExecutionID   string                     `json:"executionId"`
		Inputs        map[string]json.RawMessage `json:"inputs"`
	}
	if err := json.Unmarshal([]byte(encoded), &input); err != nil {
		t.Fatal(err)
	}
	if storedDigest != hex.EncodeToString(digest[:]) || dedupe != hex.EncodeToString(wantDedupe[:]) ||
		kind != "SERVER_EMULATIONSTATION_IMPORT" || scope != "EMULATIONSTATION_IMPORT" || year != 1971 || tagVersion != 1 ||
		input.SchemaVersion != 1 || input.ExecutionID == "" || len(input.Inputs) != 4 {
		t.Fatalf("frozen input digest=%s dedupe=%s kind=%s scope=%s year=%d tagVersion=%d input=%s", storedDigest, dedupe, kind, scope, year, tagVersion, encoded)
	}
}
