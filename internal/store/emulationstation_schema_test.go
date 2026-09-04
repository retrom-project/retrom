package store

import (
	"fmt"
	"strings"
	"testing"

	"retrom/internal/testassert"
)

func TestEmulationStationSchemaRejectsMalformedItemJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		flags    string
		metadata string
		warnings string
		manifest string
	}{
		{
			name:  "missing source flag key",
			flags: `{"hidden":false,"adult":false}`,
		},
		{
			name:  "nullable source flag",
			flags: `{"hidden":null,"adult":false,"kidGame":false}`,
		},
		{
			name: "missing metadata key",
			metadata: `{"schemaVersion":1,"title":"Schema Game","description":"","developer":"",` +
				`"publisher":"","genre":"","players":null}`,
		},
		{
			name:     "unknown warning code",
			warnings: `[{"code":"UNBOUNDED_PRIVATE_VALUE","field":"title"}]`,
		},
		{
			name:     "nullable warning member",
			warnings: `[{"code":"FIELD_IGNORED","field":null}]`,
		},
		{
			name:     "manifest unknown member",
			manifest: `{"schemaVersion":1,"contentKind":"SINGLE_FILE","files":[],"blobId":"secret"}`,
		},
		{
			name: "manifest nullable fact",
			manifest: `{"schemaVersion":1,"contentKind":"SINGLE_FILE","files":[{` +
				`"ordinal":0,"declaredKind":"FILE","relativePath":"game.nes","sizeBytes":16,"sourceFactsDigest":null}]}`,
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := openEmulationStationSchemaFixture(t)
			flags := valueOr(test.flags, `{"hidden":false,"adult":false,"kidGame":false}`)
			metadata := valueOr(test.metadata, validEmulationStationMetadataJSON())
			warnings := valueOr(test.warnings, `[]`)
			manifest := valueOr(test.manifest, validEmulationStationManifestJSON())
			fixture.insertItem(
				t,
				fmt.Sprintf("00000000-0000-7000-8000-%012d", 100+index),
				index+2,
				fmt.Sprintf("%064x", index+1),
				flags,
				metadata,
				warnings,
				manifest,
				true,
			)
		})
	}
}

func TestEmulationStationSchemaRejectsMalformedCollectionProjectionJSON(t *testing.T) {
	t.Parallel()
	fixture := openEmulationStationSchemaFixture(t)
	_, ignoredErr := fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO emulationstation_import_gamelists(
 import_id,relative_path,size_bytes,content_digest,source_facts_digest,parse_state,error_code,
 game_count,folder_count,provider_present,ignored_fields_json,ignored_field_other_count,created_at_ms
) VALUES(?,'other/gamelist.xml',1,?,?,'VALID',NULL,0,0,0,'["safe",null]',0,1)
`, fixture.importID, testDigestA, testDigestB)
	testassert.Truef(t, ignoredErr != nil, "nullable ignored field name was accepted")

	_, err := fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO emulationstation_import_gamelists(
 import_id,relative_path,size_bytes,content_digest,source_facts_digest,parse_state,error_code,
 game_count,folder_count,provider_present,ignored_fields_json,ignored_field_other_count,created_at_ms
) VALUES(?,'other/gamelist.xml',1,?,?,'VALID',NULL,0,0,0,'[]',0,1)
`, fixture.importID, testDigestA, testDigestB)
	testassert.False(t, err != nil, err)
	_, extensionErr := fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO emulationstation_import_collections(
 id,import_id,gamelist_relative_path,relative_directory,display_name,game_count,
 extension_summary_json,created_at_ms,updated_at_ms
) VALUES('invalid-extension',?,'other/gamelist.xml','other','other',0,
 '[{"extension":".nes","count":1,"privateValue":"leak"}]',1,1)
`, fixture.importID)
	testassert.Truef(t, extensionErr != nil, "unknown extension summary member was accepted")
}

func TestEmulationStationSchemaAllowsOnlyOversizedInvalidGamelistWithoutDigest(t *testing.T) {
	t.Parallel()
	fixture := openEmulationStationSchemaFixture(t)
	_, err := fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO emulationstation_import_gamelists(
 import_id,relative_path,size_bytes,content_digest,source_facts_digest,parse_state,error_code,
 game_count,folder_count,provider_present,ignored_fields_json,ignored_field_other_count,created_at_ms
) VALUES(?,'oversized/gamelist.xml',8388609,NULL,?,'INVALID',
 'EMULATIONSTATION_GAMELIST_TOO_LARGE',0,0,0,'[]',0,1)
`, fixture.importID, testDigestA)
	testassert.False(t, err != nil, err)
	_, err = fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO emulationstation_import_gamelists(
 import_id,relative_path,size_bytes,content_digest,source_facts_digest,parse_state,error_code,
 game_count,folder_count,provider_present,ignored_fields_json,ignored_field_other_count,created_at_ms
) VALUES(?,'missing-digest/gamelist.xml',1,NULL,?,'INVALID',
 'EMULATIONSTATION_GAMELIST_INVALID_XML',0,0,0,'[]',0,1)
`, fixture.importID, testDigestB)
	testassert.Truef(t, err != nil, "non-oversized gamelist without digest was accepted")
}

func TestEmulationStationSchemaFreezesDiscoveryAndChecksTerminalCounts(t *testing.T) {
	t.Parallel()
	fixture := openEmulationStationSchemaFixture(t)
	fixture.finishScan(t)

	_, childErr := fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO emulationstation_import_item_files(
 item_id,ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,state,created_at_ms,updated_at_ms
) VALUES(?,1,'FILE','late.nes',1,?,'DISCOVERED',3,3)
`, fixture.itemID, testDigestA)
	testassert.Truef(t, childErr != nil, "late discovery child insert succeeded")
	_, snapshotErr := fixture.database.SQL.ExecContext(
		t.Context(), "UPDATE emulationstation_import_items SET title='Changed' WHERE id=?", fixture.itemID,
	)
	testassert.Truef(t, snapshotErr != nil, "frozen item title update succeeded")
	_, manifestErr := fixture.database.SQL.ExecContext(
		t.Context(), "UPDATE emulationstation_import_items SET source_manifest_json='{}' WHERE id=?", fixture.itemID,
	)
	testassert.Truef(t, manifestErr != nil, "frozen item manifest update succeeded")

	_, terminalItemErr := fixture.database.SQL.ExecContext(t.Context(), `
UPDATE emulationstation_import_items
SET execution_state='CANCELLED',error_code='CANCELLED',completed_at_ms=3,version=version+1,updated_at_ms=3
WHERE id=?
`, fixture.itemID)
	testassert.False(t, terminalItemErr != nil, terminalItemErr)
	_, earlyDeleteErr := fixture.database.SQL.ExecContext(
		t.Context(), "DELETE FROM emulationstation_import_item_files WHERE item_id=?", fixture.itemID,
	)
	testassert.Truef(t, earlyDeleteErr != nil, "non-pending AWAITING_MAPPING item payload was deletable")
	_, countErr := fixture.database.SQL.ExecContext(t.Context(), `
UPDATE emulationstation_imports
SET state='EXPIRED',phase=NULL,last_error_code='EMULATIONSTATION_PLAN_EXPIRED',
 completed_at_ms=3,version=version+1,updated_at_ms=3
WHERE id=?
`, fixture.importID)
	testassert.Truef(t, countErr != nil, "terminal aggregate with stale counts succeeded")
	_, err := fixture.database.SQL.ExecContext(t.Context(), `
UPDATE emulationstation_imports
SET state='EXPIRED',phase=NULL,last_error_code='EMULATIONSTATION_PLAN_EXPIRED',cancelled_item_count=1,
 completed_at_ms=3,version=version+1,updated_at_ms=3
WHERE id=?
`, fixture.importID)
	testassert.False(t, err != nil, err)
	for _, statement := range []string{
		"DELETE FROM emulationstation_import_item_files WHERE item_id=?",
		"DELETE FROM emulationstation_import_items WHERE id=?",
		"DELETE FROM emulationstation_import_collections WHERE import_id=?",
		"DELETE FROM emulationstation_import_gamelists WHERE import_id=?",
		"DELETE FROM emulationstation_imports WHERE id=?",
	} {
		_, err = fixture.database.SQL.ExecContext(t.Context(), statement, mapDeleteArgument(statement, fixture))
		testassert.False(t, err != nil, err)
	}
}

func TestEmulationStationSchemaRejectsInvalidJobScopeAndPayloadOwner(t *testing.T) {
	t.Parallel()
	fixture := openEmulationStationSchemaFixture(t)
	_, kindErr := fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO jobs(
 id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms
) VALUES('bad-scan-scope','GAME','game','SERVER_EMULATIONSTATION_SCAN',?,1,'{}',1,'QUEUED',0,4,1,1,1)
`, testDigestA)
	testassert.Truef(t, kindErr != nil, "EmulationStation job with GAME scope succeeded")
	_, scopeErr := fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO jobs(
 id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms
) VALUES('bad-kind-scope','EMULATIONSTATION_IMPORT','scope','GAME_FILE_REVISION',?,1,'{}',1,'QUEUED',0,4,1,1,1)
`, testDigestB)
	testassert.Truef(t, scopeErr != nil, "unrelated job with EmulationStation scope succeeded")
	_, err := fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO jobs(
 id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms
) VALUES('wrong-release-owner','GAME',?,'PAYLOAD_RELEASE',?,1,'{}',0,'QUEUED',0,4,1,1,1)
`, fixture.itemID, testDigestC)
	testassert.False(t, err != nil, err)
	_, ownerErr := fixture.database.SQL.ExecContext(t.Context(), `
UPDATE emulationstation_import_items
SET payload_state='RELEASING',payload_release_job_id='wrong-release-owner',version=version+1
WHERE id=?
`, fixture.itemID)
	testassert.Truef(t, ownerErr != nil, "foreign payload release job was accepted")
}

func TestEmulationStationSchemaAllowsPendingItemToCloseAsCommitFailed(t *testing.T) {
	t.Parallel()
	fixture := openEmulationStationSchemaFixture(t)
	_, err := fixture.database.SQL.ExecContext(t.Context(), `
UPDATE emulationstation_import_items
SET execution_state='COMMIT_FAILED',error_code='EMULATIONSTATION_WORKER_ATTEMPTS_EXHAUSTED',
 retryable=0,completed_at_ms=2,version=version+1,updated_at_ms=2
WHERE id=?
`, fixture.itemID)
	testassert.False(t, err != nil, err)
}

func TestEmulationStationSchemaAllowsCopyingItemToCloseAsBlockedContent(t *testing.T) {
	t.Parallel()
	fixture := openEmulationStationSchemaFixture(t)
	_, err := fixture.database.SQL.ExecContext(t.Context(), `
UPDATE emulationstation_import_items
SET execution_state='COPYING',updated_at_ms=2
WHERE id=?
`, fixture.itemID)
	testassert.False(t, err != nil, err)
	_, err = fixture.database.SQL.ExecContext(t.Context(), `
UPDATE emulationstation_import_items
SET execution_state='BLOCKED_CONTENT',error_code='EMULATIONSTATION_CONTENT_FORMAT_UNSUPPORTED',
 retryable=0,completed_at_ms=3,version=version+1,updated_at_ms=3
WHERE id=?
`, fixture.itemID)
	testassert.False(t, err != nil, err)
}

func TestServerReviewSourceOwnershipIsMutuallyExclusive(t *testing.T) {
	t.Parallel()
	t.Run("Pegasus insert after EmulationStation link", func(t *testing.T) {
		t.Parallel()
		fixture := openEmulationStationSchemaFixture(t)
		jobID, libraryItemID := fixture.seedLibraryReview(t)
		fixture.seedPegasusImport(t)
		linkEmulationStationReview(t, fixture, jobID, libraryItemID, false)
		insertPegasusOwner(t, fixture.database.SQL, libraryItemID, true)
	})
	t.Run("EmulationStation link after Pegasus insert", func(t *testing.T) {
		t.Parallel()
		fixture := openEmulationStationSchemaFixture(t)
		jobID, libraryItemID := fixture.seedLibraryReview(t)
		fixture.seedPegasusImport(t)
		insertPegasusOwner(t, fixture.database.SQL, libraryItemID, false)
		linkEmulationStationReview(t, fixture, jobID, libraryItemID, true)
	})
}

func linkEmulationStationReview(
	t *testing.T,
	fixture emulationStationSchemaFixture,
	jobID, itemID string,
	wantError bool,
) {
	t.Helper()
	_, err := fixture.database.SQL.ExecContext(t.Context(), `
UPDATE emulationstation_import_items SET execution_state='COPYING',updated_at_ms=2 WHERE id=?
`, fixture.itemID)
	testassert.False(t, err != nil, err)
	_, err = fixture.database.SQL.ExecContext(t.Context(), `
UPDATE emulationstation_import_items
SET execution_state='VALIDATING',library_import_job_id=?,library_import_item_id=?,updated_at_ms=3
WHERE id=?
`, jobID, itemID, fixture.itemID)
	if wantError {
		testassert.Truef(t, err != nil, "cross-owned EmulationStation review link succeeded")
		return
	}
	testassert.False(t, err != nil, err)
}

func TestEmulationStationSchemaRemainsConsistent(t *testing.T) {
	t.Parallel()
	fixture := openEmulationStationSchemaFixture(t)
	testassert.False(t, fixture.database.IntegrityCheck(t.Context()) != nil, "fresh ES fixture integrity failed")
	var result string
	err := fixture.database.SQL.QueryRowContext(t.Context(), "PRAGMA integrity_check").Scan(&result)
	testassert.False(t, err != nil, err)
	testassert.Truef(t, strings.EqualFold(result, "ok"), "integrity_check = %s", result)
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func mapDeleteArgument(statement string, fixture emulationStationSchemaFixture) string {
	if strings.Contains(statement, "item_files") || strings.Contains(statement, "WHERE id=?") &&
		strings.Contains(statement, "import_items") {
		return fixture.itemID
	}
	return fixture.importID
}
