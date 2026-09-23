package libraryimport

import (
	"database/sql"
	"strings"
	"testing"

	application "retrom/internal/service/libraryimport"
)

func TestArcadeParentCommitFinishesRetryableAttachmentAtomically(t *testing.T) {
	t.Parallel()
	database := metadataDatabase(t)
	insertArcadeParentCommitTerminalFixture(t, database, "REVIEW_ARCADE_PARENT_VALIDATE", "RUNNING")

	repository := NewArcadeParentCommitRepository(database)
	err := repository.FinishRetryable(t.Context(), application.ArcadeParentRetryableCommit{
		AttachmentID: "attachment", ItemID: "item", JobID: "parent-job", WorkerID: "worker",
		Code: "REVIEW_PARENT_INPUT_STALE", DiagnosticsJSON: `{"errorCode":"REVIEW_PARENT_INPUT_STALE"}`,
		BlobSize: 12, BlobSHA: strings.Repeat("b", 64), NowMS: 20,
	})
	if err != nil {
		t.Fatal(err)
	}

	var attachmentState, attachmentCode, jobState, jobCode string
	if err := database.QueryRowContext(t.Context(), `
SELECT attachment.state,attachment.error_code,job.state,job.error_code
FROM review_arcade_parent_attachments attachment JOIN jobs job ON job.id=attachment.job_id
WHERE attachment.id='attachment'
`).Scan(&attachmentState, &attachmentCode, &jobState, &jobCode); err != nil {
		t.Fatal(err)
	}
	if attachmentState != "FAILED_RETRYABLE" || attachmentCode != "REVIEW_PARENT_INPUT_STALE" ||
		jobState != "FAILED" || jobCode != "REVIEW_PARENT_INPUT_STALE" {
		t.Fatalf("terminal states attachment=%s/%s job=%s/%s", attachmentState, attachmentCode, jobState, jobCode)
	}
	var eventCount int
	if err := database.QueryRowContext(t.Context(), `
SELECT count(*) FROM job_events WHERE job_id='parent-job' AND event_type='FAILED'
`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("failed event count=%d", eventCount)
	}
}

func TestArcadeParentCommitFinishesCancellationWithCompareAndSet(t *testing.T) {
	t.Parallel()
	database := metadataDatabase(t)
	insertArcadeParentCommitTerminalFixture(t, database, "REVIEW_ARCADE_PARENT_VALIDATE", "CANCEL_REQUESTED")

	repository := NewArcadeParentCommitRepository(database)
	ok, err := repository.FinishCancellation(t.Context(), application.ArcadeParentAttachmentCancellation{
		AttachmentID: "attachment", ItemID: "item", JobID: "parent-job", WorkerID: "worker", NowMS: 30,
	})
	if err != nil || !ok {
		t.Fatalf("finish cancellation ok=%t err=%v", ok, err)
	}

	var attachmentState, jobState string
	if err := database.QueryRowContext(t.Context(), `
SELECT attachment.state,job.state
FROM review_arcade_parent_attachments attachment JOIN jobs job ON job.id=attachment.job_id
WHERE attachment.id='attachment'
`).Scan(&attachmentState, &jobState); err != nil {
		t.Fatal(err)
	}
	if attachmentState != "CANCELLED" || jobState != "CANCELLED" {
		t.Fatalf("cancellation states attachment=%s job=%s", attachmentState, jobState)
	}
}

func insertArcadeParentCommitTerminalFixture(
	t *testing.T, database *sql.DB, kind, jobState string,
) {
	t.Helper()
	metadataExec(t, database, `
INSERT INTO dat_versions(id,core_id,provider_id,target_id,builtin_relative_path,sha256,parser_version,
parse_status,is_active,version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
SELECT 'parent-dat',instance.default_core_id,binding.provider_id,binding.target_id,'parent.xml',?,
'fixture','READY',1,1,1,1,1,1
FROM platform_instances instance JOIN runtime_target_bindings binding ON binding.core_id=instance.default_core_id
WHERE instance.id=(SELECT target_platform_instance_id FROM import_jobs WHERE id='import')`, strings.Repeat("c", 64))
	metadataExec(t, database, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,execution_started_at_ms,leased_until_ms,heartbeat_at_ms,
created_at_ms,updated_at_ms,worker_id,error_code,error_retryable,cancel_requested_at_ms)
VALUES('parent-job','IMPORT_ITEM','item',?, ?,1,'{}',1,?,1,4,1,1,1,100,1,1,1,'worker',NULL,NULL,?)
`, kind, strings.Repeat("d", 64), jobState, cancellationRequestedAt(jobState))
	metadataExec(t, database, `
INSERT INTO review_arcade_parent_attachments(
id,import_item_id,review_draft_id,base_source_snapshot_id,dependency_machine,expected_logical_name,
required_by_machine,depth,provider_id,target_id,dat_version_id,original_filename,state,diagnostics_json,
job_id,version,created_at_ms,updated_at_ms)
SELECT 'attachment','item','item','snapshot','parent','parent.zip','root',1,binding.provider_id,binding.target_id,
'parent-dat','parent.zip','RUNNING','{"schemaVersion":1}','parent-job',1,1,1
FROM runtime_target_bindings binding
WHERE binding.core_id=(SELECT default_core_id FROM platform_instances WHERE id=(SELECT target_platform_instance_id FROM import_jobs WHERE id='import'))
LIMIT 1`)
}

func cancellationRequestedAt(state string) any {
	if state == "CANCEL_REQUESTED" {
		return int64(2)
	}
	return nil
}
