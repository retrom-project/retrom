//go:build integration

package libraryimport

import (
	"errors"
	"reflect"
	"testing"
)

type (
	discardAttachmentCase     struct{ kind, state, wantAttachment, wantJob string }
	discardAttachmentSnapshot struct {
		State, JobState                              string
		Version, JobVersion                          int64
		FinishedAt, JobFinishedAt, CancelRequestedAt *int64
		ErrorCode, JobReason                         *string
	}
)

func TestDiscardAttachmentCancellationSharesDecisionTransaction(t *testing.T) {
	t.Parallel()
	for _, test := range []discardAttachmentCase{
		{"ARCADE", "QUEUED", "CANCELLED", "CANCELLED"},
		{"ARCADE", "RUNNING", "CANCELLED", "CANCEL_REQUESTED"},
		{"MULTIDISC", "RUNNING", "RUNNING", "CANCEL_REQUESTED"},
		{"MULTIDISC", "FAILED_RETRYABLE", "CANCELLED", "CANCELLED"},
	} {
		t.Run(test.kind+"/"+test.state, func(t *testing.T) { t.Parallel(); verifyDiscardAttachment(t, test) })
	}
}

func verifyDiscardAttachment(t *testing.T, test discardAttachmentCase) {
	t.Helper()
	fixture, request := ownedSourceFixture(t)
	created, err := fixture.service.CreateOwnedServerSource(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	itemID := created.Items[0].ItemID
	fixture.execute(t, `UPDATE source_import_items SET execution_state='REVIEW_PENDING',completed_at_ms=? WHERE id=?`, ownedSourceNow().UnixMilli(), request.Intent.ItemID)
	fixture.execute(t, `UPDATE source_imports SET review_pending_item_count=1 WHERE id=?`, request.Intent.ImportID)
	seedDiscardAttachment(t, fixture, itemID, test)
	before := readDiscardAttachment(t, fixture, test.kind)
	fault := newReviewDiscardFault(t, fixture, itemID, "item")
	result, err := fixture.service.Discard(t.Context(), itemID, 1, "")
	if !errors.Is(err, fault.cause) || result != (DecisionResult{}) || fault.itemWrites != 1 || fault.faults != 1 {
		t.Fatalf("attachment fault missed decision write: result=%+v err=%v writes=%d hits=%d", result, err, fault.itemWrites, fault.faults)
	}
	if after := readDiscardAttachment(t, fixture, test.kind); !reflect.DeepEqual(before, after) {
		t.Fatalf("attachment changed on rollback: before=%+v after=%+v", before, after)
	}
	fixture.service.database = fixture.database
	if _, err := fixture.service.Discard(t.Context(), itemID, 1, ""); err != nil {
		t.Fatal(err)
	}
	after := readDiscardAttachment(t, fixture, test.kind)
	if after.State != test.wantAttachment || after.JobState != test.wantJob || after.CancelRequestedAt == nil || after.JobReason == nil || *after.JobReason != "review discarded" {
		t.Fatalf("attachment cancellation changed semantics: %+v", after)
	}
}

func seedDiscardAttachment(t *testing.T, fixture deduplicateFixture, itemID string, test discardAttachmentCase) {
	t.Helper()
	jobKind := "REVIEW_ARCADE_PARENT_VALIDATE"
	if test.kind == "MULTIDISC" {
		jobKind = "REVIEW_MULTI_DISC_VALIDATE"
	}
	jobState := test.state
	var finished *int64
	var errorCode *string
	if test.state == "FAILED_RETRYABLE" {
		jobState = "FAILED"
		value := int64(1)
		finished = &value
		code := "RETRY"
		errorCode = &code
	}
	fixture.execute(t, `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,
 cancellable,state,attempt_count,max_attempts,error_retryable,finished_at_ms,available_at_ms,created_at_ms,updated_at_ms)
 VALUES('discard-attachment-job','IMPORT_ITEM',?,?,'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',1,'{}',1,?,1,4,1,?,1,1,1)`, itemID, jobKind, jobState, finished)
	if test.kind == "ARCADE" {
		insertArcadeParentCatalog(t, fixture.database)
		fixture.execute(t, `INSERT INTO review_arcade_parent_attachments(id,import_item_id,review_draft_id,
 base_source_snapshot_id,dependency_machine,expected_logical_name,required_by_machine,depth,
 provider_id,target_id,dat_version_id,original_filename,state,diagnostics_json,job_id,created_at_ms,updated_at_ms)
 SELECT 'discard-attachment',draft.id,draft.id,draft.effective_source_snapshot_id,
 'b','b.zip','a',1,dat.provider_id,dat.target_id,dat.id,'b.zip',?,'{}','discard-attachment-job',1,1
 FROM import_items draft JOIN dat_versions dat ON dat.id='attachment-dat' WHERE draft.id=?`, test.state, itemID)
		return
	}
	fixture.execute(t, `INSERT INTO review_multidisc_attachments(id,import_item_id,review_draft_id,requested_by_user_id,
 base_source_snapshot_id,upload_session_id,expected_set_digest,state,error_code,diagnostics_json,job_id,finished_at_ms,created_at_ms,updated_at_ms)
 SELECT 'discard-attachment',i.id,d.id,'owner-actor',d.effective_source_snapshot_id,j.upload_session_id,
 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',?,?,'{}','discard-attachment-job',?,1,1
 FROM import_items d JOIN import_items i ON i.id=d.id JOIN import_jobs j ON j.id=i.import_job_id WHERE i.id=?`, test.state, errorCode, finished, itemID)
}

func readDiscardAttachment(t *testing.T, fixture deduplicateFixture, kind string) discardAttachmentSnapshot {
	t.Helper()
	table := "review_arcade_parent_attachments"
	if kind == "MULTIDISC" {
		table = "review_multidisc_attachments"
	}
	var result discardAttachmentSnapshot
	if err := fixture.database.QueryRowContext(t.Context(), `
SELECT a.state,a.version,a.finished_at_ms,a.error_code,j.state,j.version,j.finished_at_ms,j.cancel_requested_at_ms,j.cancel_reason
FROM `+table+` a JOIN jobs j ON j.id=a.job_id WHERE a.id='discard-attachment'`).Scan(&result.State, &result.Version, &result.FinishedAt, &result.ErrorCode,
		&result.JobState, &result.JobVersion, &result.JobFinishedAt, &result.CancelRequestedAt, &result.JobReason); err != nil {
		t.Fatal(err)
	}
	return result
}
