//go:build integration

package libraryimport

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func assertPreviewRefreshesLegacyBIOS(t *testing.T, database *sql.DB, importer *Service, itemID, biosBlobID string) int64 {
	t.Helper()
	ctx := t.Context()
	var validationID, snapshotJSON string
	var version int64
	if err := database.QueryRowContext(ctx, `
SELECT draft.selected_validation_id,validation.dependency_snapshot_json,draft.version
FROM review_drafts draft JOIN import_item_core_validations validation ON validation.id=draft.selected_validation_id
WHERE draft.import_item_id=?
`, itemID).Scan(&validationID, &snapshotJSON, &version); err != nil {
		t.Fatal(err)
	}
	var snapshot arcadeDraftSnapshot
	if err := json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Dependencies[0].State = "MISSING"
	snapshot.MissingEntries = []string{"codexbios.zip"}
	snapshot.Warnings = []string{}
	legacyJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	metadata := `{"title":"Child","description":"` + strings.Repeat("界", 12167) + `"}`
	// Seed the immutable evidence produced before missing-entry uploads were usable.
	legacyID := uuid.NewString()
	if _, err := database.ExecContext(ctx, `
INSERT INTO import_item_core_validations(id,import_item_id,target_platform_instance_id,platform_instance_version,
core_id,provider_id,target_id,dat_version_id,default_dos_entry,source_manifest_digest,source_snapshot_id,
prepublish_input_digest,status,compatibility_code,dependency_snapshot_json,created_at_ms)
SELECT ?,import_item_id,target_platform_instance_id,platform_instance_version,core_id,provider_id,target_id,
dat_version_id,default_dos_entry,source_manifest_digest,source_snapshot_id,prepublish_input_digest,
'BLOCKED','LAUNCH_BIOS_MISSING',?,created_at_ms+1 FROM import_item_core_validations WHERE id=?;
`, legacyID, string(legacyJSON), validationID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE review_drafts SET selected_validation_id=NULL,metadata_json=? WHERE import_item_id=?`, metadata, itemID); err != nil {
		t.Fatal(err)
	}
	if err := importer.RefreshReviewPreviewValidation(ctx, itemID); err != nil {
		t.Fatal(err)
	}
	var selected, status, actualBlob, actualMetadata string
	var nextVersion int64
	if err := database.QueryRowContext(ctx, `
SELECT draft.selected_validation_id,validation.status,file.blob_id,draft.metadata_json,draft.version
FROM review_drafts draft JOIN import_item_core_validations validation ON validation.id=draft.selected_validation_id
JOIN import_item_validation_files file ON file.import_item_core_validation_id=validation.id AND file.role='BIOS_BUNDLE'
WHERE draft.import_item_id=?
`, itemID).Scan(&selected, &status, &actualBlob, &actualMetadata, &nextVersion); err != nil {
		t.Fatal(err)
	}
	if selected == legacyID || status != "READY" || actualBlob != biosBlobID || actualMetadata != metadata || nextVersion != version+1 {
		t.Fatalf("preview refresh: selected=%s status=%s blob=%s version=%d expectedVersion=%d blobEqual=%t metadataEqual=%t", selected, status, actualBlob, nextVersion, version+1, actualBlob == biosBlobID, actualMetadata == metadata)
	}
	var oldJSON string
	if err := database.QueryRowContext(ctx, `SELECT dependency_snapshot_json FROM import_item_core_validations WHERE id=?`, legacyID).Scan(&oldJSON); err != nil {
		t.Fatal(err)
	}
	if oldJSON != string(legacyJSON) {
		t.Fatal("preview refresh rewrote immutable evidence")
	}
	if err := importer.RefreshReviewPreviewValidation(ctx, itemID); err != nil {
		t.Fatal(err)
	}
	var repeatedVersion int64
	if err := database.QueryRowContext(ctx, `SELECT version FROM review_drafts WHERE import_item_id=?`, itemID).Scan(&repeatedVersion); err != nil {
		t.Fatal(err)
	}
	if repeatedVersion != nextVersion {
		t.Fatal("unchanged dependencies changed the draft version")
	}
	return nextVersion
}
