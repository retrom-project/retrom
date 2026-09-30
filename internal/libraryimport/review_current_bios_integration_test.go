//go:build integration

package libraryimport

import (
	"bytes"
	"testing"

	dbapi "retrom/internal/database"

	"retrom/internal/filestore"
	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

func TestReviewDetailReadsBIOSInstallationWithoutDraftMutation(t *testing.T) {
	fixture := newDeduplicateFixture(t)
	fixture.execute(t, `UPDATE bios_requirements SET requirement_mode='REQUIRED'
WHERE core_id='mgba' AND logical_name='gba_bios.bin'`)
	created := fixture.create(t, "current-bios", "Retrom current BIOS regression content", 1)
	itemID := created.Items[0].ItemID
	details := libraryservice.NewReviewDetails(repository.NewReviewDetail(fixture.database))
	before, err := details.Get(fixture.ctx, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if before.CanApprove || before.Readiness == nil || before.Readiness.CompatibilityCode != "LAUNCH_BIOS_MISSING" {
		t.Fatalf("missing BIOS detail: %+v", before.Readiness)
	}
	assertCurrentBIOSQueue(t, fixture, itemID, "BLOCKED", true)
	metadata, err := fixture.blobs.Put(bytes.NewBufferString("Retrom owned BIOS test vector"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := filestore.FileRecord(metadata, "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	fixture.execute(t, `INSERT INTO bios_installations(id,requirement_id,file_record,original_filename,
size_bytes,md5,sha1,sha256,validated_requirement_version,status,validation_details_json,
is_active,version,created_at_ms,updated_at_ms)
SELECT 'current-bios-installation',id,?,'gba_bios.bin',?,?,?,?,version,'HASH_WARNING','{}',1,1,1,1
FROM bios_requirements WHERE core_id='mgba' AND logical_name='gba_bios.bin'`,
		record, metadata.Size, metadata.MD5, metadata.SHA1, metadata.SHA256)
	after, err := details.Get(fixture.ctx, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.CanApprove || after.Readiness == nil || after.Readiness.Status != "READY" {
		t.Fatalf("installed BIOS not visible on read: %+v", after.Readiness)
	}
	assertCurrentBIOSQueue(t, fixture, itemID, "READY", false)
	if after.Version != before.Version {
		t.Fatalf("reading current facts changed review version: %d -> %d", before.Version, after.Version)
	}
	fixture.execute(t, `UPDATE bios_installations SET is_active=0 WHERE id='current-bios-installation'`)
	removed, err := details.Get(fixture.ctx, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if removed.CanApprove || removed.Readiness == nil || removed.Readiness.CompatibilityCode != "LAUNCH_BIOS_MISSING" {
		t.Fatalf("removed BIOS not visible on read: %+v", removed.Readiness)
	}
	if _, err := fixture.service.Approve(fixture.ctx, itemID, before.Version); err == nil {
		t.Fatal("approval accepted removed required BIOS")
	}
	var games int
	if err := dbapi.QueryRowContext(fixture.ctx, fixture.database, `SELECT count(*) FROM games`).Scan(&games); err != nil || games != 0 {
		t.Fatalf("blocked approval leaked game: count=%d error=%v", games, err)
	}
	fixture.execute(t, `UPDATE bios_installations SET is_active=1 WHERE id='current-bios-installation'`)
	if _, err := fixture.service.Approve(fixture.ctx, itemID, before.Version); err != nil {
		t.Fatalf("approval did not read reinstalled BIOS: %v", err)
	}
}

func assertCurrentBIOSQueue(t *testing.T, fixture deduplicateFixture, itemID, status string, missing bool) {
	t.Helper()
	queue := repository.NewReviewQueue(fixture.database)
	records, err := queue.List(fixture.ctx, libraryservice.ReviewQueueQuery{Filter: libraryservice.ReviewQueueFilter{Sort: "UPDATED_ASC"}, Limit: 1})
	if err != nil || len(records) != 1 || records[0].ItemID != itemID || records[0].ValidationStatus == nil || *records[0].ValidationStatus != status {
		t.Fatalf("current queue=%+v error=%v", records, err)
	}
	filtered, err := queue.List(fixture.ctx, libraryservice.ReviewQueueQuery{Filter: libraryservice.ReviewQueueFilter{BlockerCode: "LAUNCH_BIOS_MISSING", Sort: "UPDATED_ASC"}, Limit: 1})
	if err != nil || (len(filtered) == 1) != missing {
		t.Fatalf("current blocked queue=%+v error=%v", filtered, err)
	}
	candidates, err := repository.NewReviewBulkQueries(fixture.database).Candidates(fixture.ctx, libraryservice.ReviewBulkCandidateQuery{Limit: 1})
	if err != nil || len(candidates) != 1 || candidates[0].ValidationStatus == nil || *candidates[0].ValidationStatus != status {
		t.Fatalf("current bulk=%+v error=%v", candidates, err)
	}
}
