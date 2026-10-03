package libraryimport

import (
	"strings"
	"testing"

	dbapi "retrom/internal/database"
	"retrom/internal/jobinput"
	"retrom/internal/persistence/uploads/payloadpurge"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport"
)

func TestParentAdmissionRetainsReusableUploadAndWorkerUsesAdmittedInput(t *testing.T) {
	db := metadataDatabase(t)
	insertArcadeParentCommitTerminalFixture(t, db, "REVIEW_ARCADE_PARENT_VALIDATE", "QUEUED")
	metadata := testsupport.FileMetadata("parent-input")
	input := libraryservice.ArcadeParentAttachmentInput{
		SchemaVersion: 1, AttachmentID: "attachment",
		ImportItemID: "item", ReviewDraftID: "item", BaseSourceSnapshotID: "snapshot", DependencyMachine: "parent",
		DATVersionID: "parent-dat", UploadFileID: "parent-upload", UploadSessionID: "upload", FileRecord: metadata.Record,
		SHA256: metadata.SHA256, SizeBytes: metadata.Size, ContentPolicyDigest: strings.Repeat("a", 64),
	}
	if err := dbapi.QueryRowContext(t.Context(), db,
		`SELECT provider_id,target_id FROM review_arcade_parent_attachments WHERE id='attachment'`).Scan(&input.ProviderID, &input.TargetID); err != nil {
		t.Fatal(err)
	}
	metadataExec(t, db, `DELETE FROM review_arcade_parent_attachments WHERE id='attachment'`)
	metadataExec(t, db, `DELETE FROM jobs WHERE id='parent-job'`)
	metadataExec(t, db, `UPDATE upload_files SET state='COMPLETE',final_file_record=? WHERE id='parent-upload'`, metadata.Record)
	metadataExec(t, db, `INSERT INTO import_files(id,upload_session_id,relative_path,file_record,size_bytes,created_at_ms)
 VALUES('parent-upload','upload','parent.zip',?,?,1)`, metadata.Record, metadata.Size)
	metadataExec(t, db, `INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
 VALUES('other-input','upload','parent-upload','REVIEW_ARCADE_PARENT','other-attachment',1)`)
	encoded, err := jobinput.Encode("REVIEW_ARCADE_PARENT_VALIDATE", jobinput.Scope{Type: "IMPORT_ITEM", ID: "item"}, input)
	if err != nil {
		t.Fatal(err)
	}
	err = NewArcadeParentAttachments(db).WithAdmission(t.Context(), func(scope libraryservice.ArcadeParentAttachmentAdmissionScope) error {
		upload, found, err := scope.Read.Upload(t.Context(), "parent-upload")
		if err != nil {
			return err
		}
		if !found || upload.WholeSessionConsumed {
			t.Fatal("existing file consumer prevented reuse")
		}
		return scope.Write.Create(t.Context(), libraryservice.ArcadeParentAttachmentWrite{
			Input: input, InputJSON: string(encoded), InputDigest: strings.Repeat("b", 64), DedupeKey: strings.Repeat("c", 64),
			AttachmentID: "attachment", JobID: "parent-job", ItemID: "item", DraftID: "item", BaseSourceSnapshotID: "snapshot",
			DependencyMachine: "parent", RequiredByMachine: "root", Depth: 1, ProviderID: input.ProviderID, TargetID: input.TargetID,
			DATVersionID: "parent-dat", UploadID: "parent-upload", OriginalFilename: "parent.zip", ExpectedDraftVersion: 7, NowMS: 5,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	var retained int
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT count(*) FROM upload_consumptions
 WHERE upload_file_id='parent-upload' AND released_at_ms IS NULL`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != 2 {
		t.Fatalf("admission failed to retain shared input: %d", retained)
	}
	metadataExec(t, db, `UPDATE upload_consumptions SET released_at_ms=6,release_reason='UPLOAD_CONSUMED'
 WHERE id='other-input'`)
	candidates, err := (payloadpurge.Records{Executor: db}).Candidates(t.Context(), "upload", "", 20)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("other consumer released queued input: candidates=%v err=%v", candidates, err)
	}
	// A changed transport projection must not erase a durably admitted execution.
	metadataExec(t, db, `UPDATE import_files SET file_record=NULL,released_at_ms=6 WHERE id='parent-upload'`)
	claim, err := NewArcadeParentAttachmentWorker(db).Claim(t.Context(), "parent-job", "worker", 10)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Candidate.FileRecord != metadata.Record || claim.Candidate.UploadSessionID != "upload" {
		t.Fatalf("worker followed mutable transport: %+v", claim.Candidate)
	}
}
