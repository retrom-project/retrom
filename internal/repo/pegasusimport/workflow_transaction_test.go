package pegasusimport

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	pegasusimportmodel "retrom/internal/model/pegasusimport"
	pegasusimportservice "retrom/internal/service/pegasusimport"

	"github.com/google/uuid"
)

func TestWorkflowRetryRollsBackStaleIdentityAuditAndLateFailure(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"job_version", "execution", "import_version", "audit", "callback"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			db := workflowDatabase(t)
			beforeRows := workflowRows(t, db)
			cause := errors.New("late workflow failure")
			err := NewWorkflowControl(db).WithControl(t.Context(), func(scope pegasusimportmodel.WorkflowScope) error {
				before, err := scope.Read.Current(t.Context(), "import-0")
				if err != nil {
					return err
				}
				plan := pegasusimportmodel.RetryPlan{Before: before, Execution: 2, ExecutionID: "execution-2", AuditID: "audit-2", ActorID: "actor", NowMS: 10}
				switch failure {
				case "job_version":
					plan.Before.JobVersion++
				case "execution":
					plan.Before.Execution++
				case "import_version":
					plan.Before.Summary.Version++
				case "audit":
					plan.AuditID = "audit-0"
				}
				if err := scope.Write.Retry(t.Context(), plan); err != nil {
					return err
				}
				return cause
			})
			if err == nil {
				t.Fatal("invalid retry committed")
			}
			if failure == "callback" && !errors.Is(err, cause) {
				t.Fatalf("callback cause: %v", err)
			}
			if !reflect.DeepEqual(workflowRows(t, db), beforeRows) {
				t.Fatal("failed retry left partial writes")
			}
		})
	}
}

func TestWorkflowCancellationRollsBackStaleIdentityAndReleaseScheduling(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"job_version", "import_version", "audit", "callback"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			db := workflowDatabase(t)
			prepareQueuedCancellation(t, db)
			beforeRows := workflowRows(t, db)
			cause := errors.New("late cancellation failure")
			err := NewWorkflowControl(db).WithControl(t.Context(), func(scope pegasusimportmodel.WorkflowScope) error {
				before, err := scope.Read.Current(t.Context(), "import-0")
				if err != nil {
					return err
				}
				completed := int64(10)
				plan := pegasusimportmodel.CancellationPlan{Before: before, State: "CANCELLED", CompletedAtMS: &completed, Reason: "Stop", ActorID: "actor", AuditID: "cancel-audit", NowMS: 10}
				switch failure {
				case "job_version":
					plan.Before.JobVersion++
				case "import_version":
					plan.Before.Summary.Version++
				case "audit":
					plan.AuditID = "audit-0"
				}
				if err := scope.Write.Cancel(t.Context(), plan); err != nil {
					return err
				}
				return cause
			})
			if err == nil {
				t.Fatal("invalid cancellation committed")
			}
			if failure == "callback" && !errors.Is(err, cause) {
				t.Fatalf("callback cause: %v", err)
			}
			if !reflect.DeepEqual(workflowRows(t, db), beforeRows) {
				t.Fatal("failed cancellation left partial writes or releases")
			}
		})
	}
}

func TestWorkflowRetryPreservesFrozenInputsAndOnlyRestartsRetryableItems(t *testing.T) {
	t.Parallel()
	db := workflowDatabase(t)
	service := pegasusimportservice.NewWorkflowControl(NewWorkflowControl(db), func() time.Time { return time.UnixMilli(10) })
	result, err := service.Retry(t.Context(), "import-0", 1, "actor")
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "QUEUED" || result.Version != 2 || result.Counts.Failed != 1 || result.Counts.ReviewPending != 1 {
		t.Fatalf("retry summary: %#v", result)
	}
	assertRetriedItems(t, db)
	assertNewManualExecution(t, db)
	before := workflowRows(t, db)
	if _, err := service.Retry(t.Context(), "import-0", 2, "actor"); !errors.Is(err, pegasusimportmodel.ErrNotRetryable) {
		t.Fatalf("active execution retried: %v", err)
	}
	if !reflect.DeepEqual(workflowRows(t, db), before) {
		t.Fatal("second retry changed active execution")
	}
}

func assertRetriedItems(t *testing.T, db *sql.DB) {
	t.Helper()
	for i, want := range []string{"PENDING", "COMMIT_FAILED", "REVIEW_PENDING", "SKIPPED_MAPPING"} {
		var state, metadata, manifest string
		var code, details sql.NullString
		var version int64
		if err := db.QueryRowContext(t.Context(), `SELECT execution_state,metadata_json,source_manifest_json,error_code,error_details_json,version
FROM pegasus_import_items WHERE game_ordinal=?`, i).Scan(&state, &metadata, &manifest, &code, &details, &version); err != nil {
			t.Fatal(err)
		}
		if state != want || metadata != `{"frozen":"metadata"}` || manifest != `{"frozen":"manifest"}` {
			t.Fatalf("item %d lost frozen data: %s %s %s", i, state, metadata, manifest)
		}
		if i == 0 {
			if code.Valid || details.Valid || version != 2 {
				t.Fatalf("retryable item kept diagnostics: %v %v %d", code, details, version)
			}
		} else if !code.Valid || !details.Valid || version != 1 {
			t.Fatalf("unretried item changed: %v %v %d", code, details, version)
		}
	}
}

func assertNewManualExecution(t *testing.T, db *sql.DB) {
	t.Helper()
	var execution, attempt, available, version int64
	var reset bool
	if err := db.QueryRowContext(t.Context(), `SELECT execution_no,attempt_count,available_at_ms,version,
execution_started_at_ms IS NULL AND execution_deadline_at_ms IS NULL AND leased_until_ms IS NULL
AND heartbeat_at_ms IS NULL AND finished_at_ms IS NULL AND worker_id IS NULL AND error_code IS NULL
AND error_retryable IS NULL AND cancel_requested_at_ms IS NULL AND cancel_reason IS NULL
FROM jobs WHERE id='work'`).Scan(&execution, &attempt, &available, &version, &reset); err != nil {
		t.Fatal(err)
	}
	if execution != 2 || attempt != 0 || available != 10 || version != 4 || !reset {
		t.Fatalf("manual execution: %d %d %d %d reset=%v", execution, attempt, available, version, reset)
	}
	var old, input, digest, actor string
	if err := db.QueryRowContext(t.Context(), `SELECT
(SELECT input_json FROM job_input_snapshots WHERE job_id='work' AND execution_no=1),input_json,input_digest,
(SELECT actor_user_id FROM audit_events WHERE action='PEGASUS_IMPORT_RETRIED')
FROM job_input_snapshots WHERE job_id='work' AND execution_no=2`).Scan(&old, &input, &digest, &actor); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(input))
	var decoded struct {
		ExecutionID string `json:"executionId"`
	}
	if err := json.Unmarshal([]byte(input), &decoded); err != nil {
		t.Fatal(err)
	}
	executionID, err := uuid.Parse(decoded.ExecutionID)
	if err != nil || executionID.Version() != 7 {
		t.Fatalf("invalid execution identity: %q %v", decoded.ExecutionID, err)
	}
	if old != `{"old":true}` || digest != hex.EncodeToString(sum[:]) || actor != "actor" {
		t.Fatalf("retry evidence: %s %s %s %s", old, input, digest, actor)
	}
}

func TestQueuedCancellationKeepsReviewItemsAndSchedulesTerminalPayloads(t *testing.T) {
	t.Parallel()
	db := workflowDatabase(t)
	prepareQueuedCancellation(t, db)
	service := pegasusimportservice.NewWorkflowControl(NewWorkflowControl(db), func() time.Time { return time.UnixMilli(10) })
	result, pending, err := service.Cancel(t.Context(), "import-0", 1, "Stop", "actor")
	if err != nil {
		t.Fatal(err)
	}
	if pending || result.State != "CANCELLED" || result.Counts.Cancelled != 1 || result.Counts.ReviewPending != 1 {
		t.Fatalf("cancel result: %#v pending=%v", result, pending)
	}
	var state, payload string
	var version, releases int
	if err := db.QueryRowContext(t.Context(), `SELECT execution_state,payload_state,version,
(SELECT count(*) FROM jobs WHERE kind='PAYLOAD_RELEASE') FROM pegasus_import_items WHERE id='item-2'`).Scan(&state, &payload, &version, &releases); err != nil {
		t.Fatal(err)
	}
	if state != "REVIEW_PENDING" || payload != "RETAINED" || version != 1 || releases != 3 {
		t.Fatalf("review preservation: %s %s v%d releases=%d", state, payload, version, releases)
	}
}
