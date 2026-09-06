package importdiscard

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/testsupport"
)

func (f *fixture) sourceCollection(t *testing.T, batch string) string {
	t.Helper()
	collection, _ := uuid.NewV7()
	now := f.now().UnixMilli()
	f.exec(t, `INSERT INTO pegasus_import_collections(id,import_id,metadata_relative_path,segment_ordinal,
name,game_count,mapping_action,target_platform_instance_id,target_platform_instance_version,
target_platform_id,target_default_core_id,target_provider_id,target_id,created_at_ms,updated_at_ms)
SELECT ?,?,'metadata',0,'Legacy',1,'IMPORT',instance.id,instance.version,instance.platform_id,instance.default_core_id,
binding.provider_id,binding.target_id,?,? FROM platform_instances instance
JOIN runtime_target_bindings binding ON binding.core_id=instance.default_core_id WHERE instance.id=?`,
		collection.String(), batch, now, now, testsupport.MustPlatformInstanceID(t, f.db, "nes/fceumm"))
	return collection.String()
}

func TestDiscardRecoversUniqueLegacyPegasusEnvelope(t *testing.T) {
	f := newFixture(t)
	file := f.file(t, "legacy.txt", 21)
	batch, item := f.source(t, "PEGASUS", file)
	result := f.create(t, file)
	if _, err := f.service.Request(f.ctx, "PEGASUS", batch, adminID); err != nil {
		t.Fatal(err)
	}
	f.finish(t, "PEGASUS", batch)
	if f.count(t, `SELECT count(*) FROM server_import_upload_owners owner JOIN import_jobs job
ON job.upload_session_id=owner.upload_session_id WHERE owner.source_item_id=? AND job.id=?`,
		item, result.Created.ImportJobID) != 1 {
		t.Fatal("legacy envelope owner was not recovered")
	}
	if f.count(t, `SELECT count(*) FROM upload_files WHERE final_blob_id=?`, file.BlobID) != 0 {
		t.Fatal("legacy rejected upload still protects bytes")
	}
}

func TestDiscardRefusesAmbiguousLegacyPegasusOwner(t *testing.T) {
	f := newFixture(t)
	file := f.file(t, "legacy.txt", 22)
	for range 2 {
		batch, _ := f.source(t, "PEGASUS", file)
		if _, err := f.service.Request(f.ctx, "PEGASUS", batch, adminID); err != nil {
			t.Fatal(err)
		}
	}
	f.create(t, file)
	if _, err := f.service.RunOnce(f.ctx); !errors.Is(err, errAmbiguousOwner) {
		t.Fatalf("ambiguous owner: %v", err)
	}
	if f.count(t, `SELECT count(*) FROM upload_files WHERE final_blob_id=?`, file.BlobID) != 1 {
		t.Fatal("ambiguous envelope was released")
	}
	if f.count(t, `SELECT count(*) FROM import_batch_discards WHERE state='FAILED'
AND error_code='IMPORT_BATCH_DISCARD_OWNER_AMBIGUOUS'`) != 1 {
		t.Fatal("missing recoverable failure status")
	}
}
