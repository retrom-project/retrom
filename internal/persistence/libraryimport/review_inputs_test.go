package libraryimport

import (
	"testing"

	dbapi "retrom/internal/database"
	"retrom/internal/testsupport"
)

func TestReviewInputsLoadsCurrentRuntimeBinding(t *testing.T) {
	t.Parallel()
	database := metadataDatabase(t)
	instance := testsupport.MustPlatformInstanceID(t, database, "gba/mgba")
	var coreID, providerID, targetID string
	if err := dbapi.QueryRowContext(t.Context(), database, `
SELECT instance.default_core_id,binding.provider_id,binding.target_id
FROM platform_instances instance
JOIN runtime_target_bindings binding ON binding.core_id=instance.default_core_id
WHERE instance.id=? LIMIT 1`, instance).Scan(&coreID, &providerID, &targetID); err != nil {
		t.Fatal(err)
	}
	repository := BindReviewInputs(database)
	assertReviewInputs(t, repository, instance, coreID, providerID, targetID)
}

func assertReviewInputs(
	t *testing.T,
	repository *ReviewInputs,
	instance, coreID, providerID, targetID string,
) {
	t.Helper()
	inputs, err := repository.Inputs(t.Context(), "item", instance)
	if err != nil {
		t.Fatal(err)
	}
	if inputs.DraftID != "item" || inputs.EffectiveSnapshotID != "snapshot" ||
		inputs.ContentKind != "SINGLE_FILE" || inputs.CoreID != coreID ||
		inputs.ProviderID != providerID || inputs.RuntimeTargetID != targetID ||
		inputs.DATVersionID != nil || !inputs.ContentPolicy.Supports("SINGLE_FILE") {
		t.Fatalf("typed inputs=%#v", inputs)
	}
}

func TestReviewInputsOrdersContentIdentity(t *testing.T) {
	t.Parallel()
	database := metadataDatabase(t)
	metadataExec(t, database, `
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,
final_file_record,state,created_at_ms,updated_at_ms)
VALUES('content-file','upload','game.gba',1,1,'content-blob','COMPLETE',1,1)`)
	metadataExec(t, database, `
INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,upload_file_id,
file_record,sort_order,created_at_ms)
VALUES('snapshot','DOS_SOURCE','dos.exe','content-file','content-blob',0,1),
('snapshot','CONTENT','game.gba','content-file','content-blob',1,1)`)
	logicalName, err := BindReviewInputs(database).ContentLogicalName(t.Context(), "snapshot")
	if err != nil || logicalName != "game.gba" {
		t.Fatalf("logical name=%q err=%v", logicalName, err)
	}
}
