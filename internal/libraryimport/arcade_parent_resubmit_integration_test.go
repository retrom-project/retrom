//go:build integration

package libraryimport

import (
	"testing"
	"time"

	dbapi "retrom/internal/database"
	jobpersistence "retrom/internal/persistence/jobs"
	"retrom/internal/service/jobs"
)

func TestCancelledParentAcceptsSameContentFromNewUpload(t *testing.T) {
	for _, running := range []bool{false, true} {
		name := "queued"
		if running {
			name = "running"
		}
		t.Run(name, func(t *testing.T) { testParentResubmission(t, running) })
	}
}

func testParentResubmission(t *testing.T, running bool) {
	t.Helper()
	fixture := newAttachmentRecoveryFixture(t, "REVIEW_ARCADE_PARENT_VALIDATE")
	if running {
		fixture.claimAndAbandon(t, time.Now().UnixMilli())
	}
	jobService := jobs.New(jobpersistence.New(fixture.database.SQL), time.Now)
	if _, _, err := jobService.Cancel(fixture.ctx, fixture.jobID, "resubmit regression"); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Now().Add(2 * time.Minute) }
	importer := newTestImporter(t, fixture.database.SQL, fixture.files, testImportOptions{Now: now})
	importer.Close()
	if err := importer.RecoverAttachmentJobs(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	var oldState, attachmentState string
	if err := dbapi.QueryRowContext(fixture.ctx, fixture.database.SQL, `
SELECT job.state,attachment.state FROM jobs job JOIN review_arcade_parent_attachments attachment
ON attachment.job_id=job.id WHERE job.id=?`, fixture.jobID).Scan(&oldState, &attachmentState); err != nil {
		t.Fatal(err)
	}
	if oldState != "CANCELLED" || attachmentState != "CANCELLED" {
		t.Fatalf("cancelled state: %s/%s", oldState, attachmentState)
	}
	uploadID := completeMultiDiscUpload(t, fixture.ctx, fixture.database, fixture.files, fixture.root, "FILES",
		[]multiDiscUploadFile{{path: "b.zip", contents: arcadeZIP(t, "b.bin", []byte("parent"))}})
	var fileID, snapshot string
	var version int64
	if err := dbapi.QueryRowContext(fixture.ctx, fixture.database.SQL, `SELECT id FROM upload_files WHERE upload_session_id=?`, uploadID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.QueryRowContext(fixture.ctx, fixture.database.SQL, `SELECT review_version,effective_source_snapshot_id FROM import_items WHERE id=?`, fixture.itemID).Scan(&version, &snapshot); err != nil {
		t.Fatal(err)
	}
	request := ParentAttachmentRequest{BaseSourceSnapshotID: snapshot, DependencyMachine: "b", UploadFileID: fileID}
	created, err := importer.CreateArcadeParentAttachment(fixture.ctx, fixture.itemID, version, request)
	if err != nil {
		t.Fatalf("same bytes, new upload rejected after cancellation: %v", err)
	}
	if created.JobID == fixture.jobID {
		t.Fatal("reused cancelled execution")
	}
	if _, err := importer.CreateArcadeParentAttachment(fixture.ctx, fixture.itemID, version, request); ParentAttachmentErrorCode(err) != ParentErrorVersion {
		t.Fatalf("duplicate admission: %v", err)
	}
	restarted := newTestImporter(t, fixture.database.SQL, fixture.files, testImportOptions{Now: now})
	restarted.ResumeAttachmentJob(fixture.ctx, "REVIEW_ARCADE_PARENT_VALIDATE", created.JobID)
	waitParentJob(t, fixture.database.SQL, created.JobID, "SUCCEEDED")
	var cancelled, violations int
	if err := dbapi.QueryRowContext(fixture.ctx, fixture.database.SQL, `SELECT count(*) FROM jobs WHERE id=? AND state='CANCELLED'`, fixture.jobID).Scan(&cancelled); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.QueryRowContext(fixture.ctx, fixture.database.SQL, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
		t.Fatal(err)
	}
	if cancelled != 1 || violations != 0 {
		t.Fatalf("history/integrity: cancelled=%d violations=%d", cancelled, violations)
	}
	fixture.assertArchiveIndexes(t)
}
