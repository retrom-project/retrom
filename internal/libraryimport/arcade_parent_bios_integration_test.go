//go:build integration

package libraryimport

import (
	"bytes"
	"strings"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	librarypersistence "retrom/internal/persistence/libraryimport"
	"retrom/internal/persistence/recordstore"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport"
)

func TestParentAttachmentAcceptsSeparateBIOSAndApprovalReadsCurrentInstallation(t *testing.T) {
	fixture := newAttachmentRecoveryFixture(t, "REVIEW_ARCADE_PARENT_VALIDATE")
	sql := fixture.database.SQL
	for _, query := range []string{
		`UPDATE dat_machines SET cloneof=NULL,romof='c' WHERE dat_version_id='attachment-dat' AND machine_name='b'`,
		`UPDATE dat_machines SET classification='EXPLICIT_BIOS',is_explicit_bios=1 WHERE dat_version_id='attachment-dat' AND machine_name='c'`,
		`INSERT INTO dat_rom_entries(dat_version_id,machine_name,ordinal,name,size_bytes,crc32,sha1,status,merge_name)
   SELECT dat_version_id,'b',1,name,size_bytes,crc32,sha1,status,name FROM dat_rom_entries WHERE dat_version_id='attachment-dat' AND machine_name='c'`,
	} {
		if _, err := sql.ExecContext(fixture.ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	importer := newTestImporter(t, sql, fixture.files, testImportOptions{Now: time.Now})
	importer.ResumeAttachmentJob(fixture.ctx, fixture.kind, fixture.jobID)
	waitParentJob(t, sql, fixture.jobID, "SUCCEEDED")
	details := libraryservice.NewReviewDetails(librarypersistence.NewReviewDetail(sql))
	missing, err := details.Get(fixture.ctx, fixture.itemID)
	if err != nil {
		t.Fatal(err)
	}
	if missing.CanApprove || missing.Readiness.CompatibilityCode != "LAUNCH_BIOS_MISSING" {
		t.Fatalf("missing BIOS: %+v", missing.Readiness)
	}
	fixture.assertArchiveIndexes(t)
	installParentFixtureBIOS(t, fixture)
	ready, err := details.Get(fixture.ctx, fixture.itemID)
	if err != nil {
		t.Fatal(err)
	}
	if !ready.CanApprove || ready.Readiness.Status != "READY" || ready.Version != missing.Version {
		t.Fatalf("current BIOS: %+v", ready.Readiness)
	}
	for _, active := range []int{0, 1} {
		if _, err = sql.ExecContext(fixture.ctx, `UPDATE bios_installations SET is_active=?,version=version+1 WHERE id='parent-bios-install'`, active); err != nil {
			t.Fatal(err)
		}
		approved, approveErr := importer.Approve(fixture.ctx, fixture.itemID, ready.Version)
		if active == 0 && approveErr == nil {
			t.Fatal("approved after BIOS removal")
		}
		if active == 1 && (approveErr != nil || approved.GameID == "") {
			t.Fatalf("approval after reinstall: %+v %v", approved, approveErr)
		}
	}
	var count int
	if err = dbapi.QueryRowContext(fixture.ctx, sql, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("foreign keys: %d %v", count, err)
	}
}

func installParentFixtureBIOS(t *testing.T, fixture attachmentRecoveryFixture) {
	t.Helper()
	metadata, err := fixture.files.Put(bytes.NewReader(arcadeZIP(t, "c.bin", []byte("root"))))
	if err != nil {
		t.Fatal(err)
	}
	record, err := filestore.FileRecord(metadata, "application/zip")
	if err != nil {
		t.Fatal(err)
	}
	target, err := testsupport.LookupRuntimeTarget(fixture.ctx, fixture.database.SQL, "fbneo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.database.SQL.ExecContext(fixture.ctx, `INSERT INTO bios_requirements(id,core_id,provider_id,target_id,source_kind,dat_machine_name,logical_name,
 requirement_mode,condition_code,activation_options_json,catalog_digest,source_url,source_version,enabled,version,created_at_ms,updated_at_ms,delivery_kind)
 VALUES('parent-bios-requirement','fbneo',?,?,'DAT_MACHINE','c','c.zip','REQUIRED','ARCADE_DAT_DEPENDENCY','{}',?,'test://bios','attachment-dat',1,1,1,1,'BIOS_BUNDLE')`, target.ProviderID, target.TargetID, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	_, err = recordstore.InsertRows(fixture.ctx, fixture.database.SQL, "bios_installations", `INSERT INTO bios_installations(id,requirement_id,file_record,original_filename,size_bytes,md5,sha1,sha256,
 validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
 VALUES('parent-bios-install','parent-bios-requirement',?,'c.zip',?,?,?,?,1,'MATCHED','{}',1,1,1,1)`, record, metadata.Size, metadata.MD5, metadata.SHA1, metadata.SHA256)
	if err != nil {
		t.Fatal(err)
	}
}
