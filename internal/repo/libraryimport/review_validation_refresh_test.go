package libraryimport

import (
	"database/sql"
	"strings"
	"testing"

	"retrom/internal/capability/content/corevalidation"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/testkit/testsupport"
)

func TestReviewValidationRefreshRepositoryLoadsTypedInputsAndCandidates(t *testing.T) {
	t.Parallel()
	database := metadataDatabase(t)
	instance := testsupport.MustPlatformInstanceID(t, database, "gba/mgba")
	var coreID, providerID, targetID string
	if err := database.QueryRowContext(t.Context(), `
SELECT instance.default_core_id,binding.provider_id,binding.target_id
FROM platform_instances instance
JOIN runtime_target_bindings binding ON binding.core_id=instance.default_core_id
WHERE instance.id=? LIMIT 1`, instance).Scan(&coreID, &providerID, &targetID); err != nil {
		t.Fatal(err)
	}
	insertRefreshSource(t, database, instance, coreID, providerID, targetID)
	repository := BindReviewValidation(database)
	assertRefreshInputs(t, repository, instance, coreID, providerID, targetID)
	assertRefreshCandidates(t, repository, instance, coreID, providerID, targetID)
}

func insertRefreshSource(t *testing.T, database *sql.DB, instance, coreID, providerID, targetID string) {
	t.Helper()
	digest := strings.Repeat("a", 64)
	metadataExec(t, database, `
INSERT INTO import_item_core_validations(
id,import_item_id,target_platform_instance_id,platform_instance_version,core_id,provider_id,target_id,
source_manifest_digest,prepublish_input_digest,status,compatibility_code,dependency_snapshot_json,created_at_ms,source_snapshot_id
)
VALUES('refresh-source','item',?,?,?,?,?,?,?,'READY','READY','{}',2,'snapshot')`,
		instance, 1, coreID, providerID, targetID, digest, digest)
}

func assertRefreshInputs(
	t *testing.T,
	repository *ReviewValidation,
	instance, coreID, providerID, targetID string,
) {
	t.Helper()
	inputs, err := repository.Inputs(t.Context(), "item", instance)
	if err != nil {
		t.Fatal(err)
	}
	if inputs.DraftID != "draft" || inputs.EffectiveSnapshotID != "snapshot" ||
		inputs.ContentKind != "SINGLE_FILE" || inputs.CoreID != coreID ||
		inputs.ProviderID != providerID || inputs.RuntimeTargetID != targetID ||
		inputs.DATVersionID != nil || inputs.PlatformID == "" || inputs.PlatformVersion < 1 ||
		inputs.DependencyFactsDigest == "" || !inputs.ContentPolicy.Supports("SINGLE_FILE") {
		t.Fatalf("typed inputs=%#v", inputs)
	}
}

func TestReviewValidationRefreshInputsTrackPlatformVersion(t *testing.T) {
	t.Parallel()
	database := metadataDatabase(t)
	instance := testsupport.MustPlatformInstanceID(t, database, "gba/mgba")
	repository := BindReviewValidation(database)
	before, err := repository.Inputs(t.Context(), "item", instance)
	if err != nil {
		t.Fatal(err)
	}
	metadataExec(t, database, `UPDATE platform_instances SET version=version+1 WHERE id=?`, instance)
	after, err := repository.Inputs(t.Context(), "item", instance)
	if err != nil {
		t.Fatal(err)
	}
	if after.PlatformVersion != before.PlatformVersion+1 || after.DependencyFactsDigest == "" {
		t.Fatalf("platform facts before=%#v after=%#v", before, after)
	}
}

func assertRefreshCandidates(
	t *testing.T,
	repository *ReviewValidation,
	instance, coreID, providerID, targetID string,
) {
	t.Helper()
	lookup := application.ReviewValidationRefreshLookup{
		ItemID: "item", SourceSnapshotID: "snapshot", TargetPlatformInstanceID: instance,
		CoreID: coreID, ProviderID: providerID, TargetID: targetID,
	}
	exact, found, err := repository.Exact(t.Context(), lookup)
	if err != nil || !found || exact.ID != "refresh-source" || exact.Status != "READY" {
		t.Fatalf("exact=%#v found=%t err=%v", exact, found, err)
	}
	fallback, found, err := repository.Fallback(t.Context(), lookup)
	if err != nil || !found || fallback.ID != exact.ID {
		t.Fatalf("fallback=%#v found=%t err=%v", fallback, found, err)
	}
}

func TestReviewValidationRefreshRepositoryWritesValidationAndBIOSFiles(t *testing.T) {
	t.Parallel()
	database := metadataDatabase(t)
	instance := testsupport.MustPlatformInstanceID(t, database, "gba/mgba")
	var coreID, providerID, targetID string
	if err := database.QueryRowContext(t.Context(), `
SELECT instance.default_core_id,binding.provider_id,binding.target_id
FROM platform_instances instance
JOIN runtime_target_bindings binding ON binding.core_id=instance.default_core_id
WHERE instance.id=? LIMIT 1`, instance).Scan(&coreID, &providerID, &targetID); err != nil {
		t.Fatal(err)
	}
	sourceDigest := strings.Repeat("a", 64)
	digest := strings.Repeat("b", 64)
	metadataExec(t, database, `
INSERT INTO import_item_core_validations(
id,import_item_id,target_platform_instance_id,platform_instance_version,core_id,provider_id,target_id,
source_manifest_digest,prepublish_input_digest,status,compatibility_code,dependency_snapshot_json,created_at_ms,source_snapshot_id
)
VALUES('refresh-source','item',?,?,?,?,?,?,?,'READY','READY','{}',2,'snapshot')`,
		instance, 1, coreID, providerID, targetID, sourceDigest, sourceDigest)
	metadataExec(t, database, `
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES('bios-blob',?,?,?,?,?,?,1)`, digest, 1, strings.Repeat("c", 32), strings.Repeat("d", 40), strings.Repeat("e", 8), "application/octet-stream")
	newID := "refresh-created"
	repository := BindReviewValidation(database)
	if err := repository.Create(t.Context(), application.ReviewValidationRefreshCreate{
		ID: newID, ItemID: "item", TargetPlatformInstanceID: instance,
		PlatformInstanceVersion: 1, CoreID: coreID, ProviderID: providerID, TargetID: targetID,
		SourceManifestDigest: sourceDigest, SourceSnapshotID: "snapshot", PrepublishInputDigest: sourceDigest,
		Status: "READY", CompatibilityCode: "READY", DependencySnapshotJSON: `{}`,
		CreatedAtMS: 3,
	}); err != nil {
		t.Fatal(err)
	}
	blobID := "bios-blob"
	if err := repository.CopyFiles(t.Context(), application.ReviewValidationRefreshFileCopy{
		ValidationID: newID, SourceValidationID: "refresh-source", CreatedAtMS: 3,
		ReplaceBIOSBundle: true,
		Dependencies: []corevalidation.BIOSDependency{{
			BIOSCatalogEntry: corevalidation.BIOSCatalogEntry{LogicalName: "bios.bin", DeliveryKind: "BIOS_BUNDLE"},
			BlobID:           &blobID,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	var role, logicalName, storedBlob string
	if err := database.QueryRowContext(t.Context(), `
SELECT role,logical_name,blob_id FROM import_item_validation_files
WHERE import_item_core_validation_id=?`, newID).Scan(&role, &logicalName, &storedBlob); err != nil {
		t.Fatal(err)
	}
	if role != "BIOS_BUNDLE" || logicalName != "bios.bin" || storedBlob != blobID {
		t.Fatalf("validation file=%s/%s/%s", role, logicalName, storedBlob)
	}
}

func TestReviewValidationRefreshRepositoryOrdersContentIdentity(t *testing.T) {
	t.Parallel()
	database := metadataDatabase(t)
	digest := strings.Repeat("f", 64)
	metadataExec(t, database, `
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES('content-blob',?,?,?,?,?,?,1)`, digest, 1, strings.Repeat("a", 32), strings.Repeat("b", 40), strings.Repeat("c", 8), "application/octet-stream")
	metadataExec(t, database, `
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,created_at_ms,updated_at_ms)
VALUES('content-file','upload','game.gba',1,1,'content-blob','COMPLETE',1,1)`)
	metadataExec(t, database, `
INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,upload_file_id,blob_id,sort_order,created_at_ms)
VALUES('snapshot','DOS_SOURCE','dos.exe','content-file','content-blob',0,1),
('snapshot','CONTENT','game.gba','content-file','content-blob',1,1)`)
	logicalName, err := BindReviewValidation(database).ContentLogicalName(t.Context(), "snapshot")
	if err != nil || logicalName != "game.gba" {
		t.Fatalf("logical name=%q err=%v", logicalName, err)
	}
}
