package libraryimport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/jobinput"
	jobpersistence "retrom/internal/persistence/jobs"
	"retrom/internal/service/jobs"
)

func TestAttachmentGenericRetryPreservesDomainInputAndProjectsFreshExecution(t *testing.T) {
	for _, kind := range []string{"REVIEW_ARCADE_PARENT_VALIDATE", "REVIEW_MULTI_DISC_VALIDATE"} {
		t.Run(kind, func(t *testing.T) {
			database := attachmentExecutionFixture(t, kind, "RUNNING")
			metadataExec(t, database, `UPDATE jobs SET state='FAILED',error_retryable=1,error_code='TEMPORARY',finished_at_ms=10 WHERE id='parent-job'`)
			scope := jobinput.Scope{Type: "IMPORT_ITEM", ID: "item"}
			input, err := jobinput.Encode(kind, scope, map[string]any{"attachmentId": "attachment", "baseSnapshotId": "snapshot", "limits": map[string]int{"maxBytes": 12}})
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(input)
			metadataExec(t, database, `INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES('parent-job',1,?,?,1)`, string(input), hex.EncodeToString(digest[:]))
			assertProjectedAttachmentState(t, database, kind, "FAILED_RETRYABLE")
			service := jobs.New(jobpersistence.New(database), func() time.Time { return time.UnixMilli(20) })
			result, err := service.Retry(t.Context(), "parent-job", 1)
			if err != nil || result.ExecutionNo != 2 {
				t.Fatalf("retry=%+v err=%v", result, err)
			}
			assertProjectedAttachmentState(t, database, kind, "QUEUED")
			assertAttachmentRetryEvidence(t, database, kind, scope, input)
			if _, err := service.Retry(t.Context(), "parent-job", 1); err == nil {
				t.Fatal("stale retry accepted")
			}
		})
	}
}

func assertProjectedAttachmentState(t *testing.T, database dbapi.DB, kind, want string) {
	t.Helper()
	records := BindReviewDependencies(database)
	if kind == "REVIEW_ARCADE_PARENT_VALIDATE" {
		rows, err := records.ArcadeAttachments(t.Context(), "item")
		if err != nil || len(rows) != 1 || rows[0].State != want {
			t.Fatalf("parent projection=%+v err=%v", rows, err)
		}
	} else {
		rows, err := records.MultiDiscAttachments(t.Context(), "item")
		if err != nil || len(rows) != 1 || rows[0].State != want {
			t.Fatalf("disc projection=%+v err=%v", rows, err)
		}
	}
}

func assertAttachmentRetryEvidence(t *testing.T, database dbapi.DB, kind string, scope jobinput.Scope, input []byte) {
	t.Helper()
	var fresh, saved, storedDigest string
	var attempt int
	var deadline *int64
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT input_json,input_digest FROM job_input_snapshots WHERE job_id='parent-job' AND execution_no=2`).Scan(&fresh, &storedDigest); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT input_json FROM job_input_snapshots WHERE job_id='parent-job' AND execution_no=1`).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT attempt_count,execution_deadline_at_ms FROM jobs WHERE id='parent-job'`).Scan(&attempt, &deadline); err != nil {
		t.Fatal(err)
	}
	before, err := jobinput.Decode(input, kind, scope)
	if err != nil {
		t.Fatal(err)
	}
	after, err := jobinput.Decode([]byte(fresh), kind, scope)
	if err != nil {
		t.Fatal(err)
	}
	freshDigest := sha256.Sum256([]byte(fresh))
	if saved != string(input) || !bytes.Equal(before.Inputs, after.Inputs) || before.ExecutionID == after.ExecutionID ||
		attempt != 0 || deadline != nil || storedDigest != hex.EncodeToString(freshDigest[:]) {
		t.Fatalf("retry changed frozen evidence or retained budget: %s -> %s attempt=%d deadline=%v", saved, fresh, attempt, deadline)
	}
}
