package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	contentcapability "retrom/internal/content/capability"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/contentquery"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport"
)

func TestParentArchiveIndexesRollBackWithSnapshotFailure(t *testing.T) {
	db := metadataDatabase(t)
	insertArcadeParentCommitTerminalFixture(t, db, "REVIEW_ARCADE_PARENT_VALIDATE", "RUNNING")
	metadataExec(t, db, "UPDATE jobs SET execution_deadline_at_ms=2000 WHERE id='parent-job'")
	source := testsupport.FileMetadata("parent-source").Record
	destination := testsupport.FileMetadata("parent-copy").Record
	metadataExec(t, db, `INSERT INTO archive_entries(archive_file_record,ordinal,original_relative_path,
 normalized_path,ascii_casefold_path,archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,created_at_ms)
 VALUES(?,0,'a.bin','a.bin','a.bin','ZIP','STORE',1,'12345678',?,?,?,1)`, source, strings.Repeat("a", 32), strings.Repeat("b", 40), strings.Repeat("c", 64))
	candidate := libraryservice.ArcadeParentCommitCandidate{
		AttachmentID: "attachment", ItemID: "item", DraftID: "item", BaseSnapshotID: "snapshot", DATID: "parent-dat",
	}
	var policy contentcapability.Policy
	var kind string
	err := dbapi.QueryRowContext(t.Context(), db, `SELECT binding.provider_id,binding.target_id,
 source.content_kind,`+contentquery.BindingPolicySQL+`
 FROM import_items item JOIN import_item_source_snapshots source ON source.id=item.effective_source_snapshot_id
 JOIN platform_instances p ON p.id=item.target_platform_instance_id
 JOIN runtime_target_bindings binding ON binding.core_id=p.default_core_id WHERE item.id='item'`).
		Scan(&candidate.ProviderID, &candidate.TargetID, &kind, contentquery.ScanPolicy(&policy))
	if err != nil {
		t.Fatal(err)
	}
	candidate.ContentPolicyDigest = policy.DigestFor(kind)
	cause := errors.New("snapshot insert failed")
	copied := false
	fault := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, _ []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.Contains(query, "INSERT INTO archive_entries") {
				copied = true
			}
			return result, nil
		},
		BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.Contains(query, "INSERT INTO import_item_source_snapshots") {
				return cause
			}
			return nil
		},
	})
	err = NewArcadeParentCommitRepository(fault).CommitAccepted(t.Context(), libraryservice.ArcadeParentAcceptedCommit{
		Candidate: candidate, JobID: "parent-job", WorkerID: "worker", NowMS: 20,
		FileCopies: map[string]string{source: destination},
	})
	if !errors.Is(err, cause) || !copied {
		t.Fatalf("copy-before-failure=%t error=%v", copied, err)
	}
	var originals, copies, snapshots int
	var effective, state string
	err = dbapi.QueryRowContext(t.Context(), db, `SELECT
 (SELECT count(*) FROM archive_entries WHERE archive_file_record=?),
 (SELECT count(*) FROM archive_entries WHERE archive_file_record=?),
 (SELECT count(*) FROM import_item_source_snapshots),
 effective_source_snapshot_id,(SELECT state FROM review_arcade_parent_attachments WHERE id='attachment')
 FROM import_items WHERE id='item'`, source, destination).Scan(&originals, &copies, &snapshots, &effective, &state)
	if err != nil || originals != 1 || copies != 0 || snapshots != 1 || effective != "snapshot" || state != "PENDING" {
		t.Fatalf("partial accepted snapshot: originals=%d copies=%d snapshots=%d effective=%s state=%s err=%v",
			originals, copies, snapshots, effective, state, err)
	}
}
