package emulationstationimport

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
)

func TestStartRejectsUnavailableIdentitiesBeforeWriting(t *testing.T) {
	fixture, mapped, _ := queryMappedTagFixture(t)
	var result Summary
	var err error
	func() {
		uuid.SetRand(creationEntropyFailure{})
		defer uuid.SetRand(nil)
		result, err = fixture.service.StartImport(fixture.context, mapped.ID, mapped.Version)
	}()
	if result.ID != "" || !errors.Is(err, errCreationEntropy) {
		t.Fatalf("unavailable identity queued result=%#v error=%v", result, err)
	}
	assertNextStartUnchanged(t, fixture, mapped)
}

func TestStartRechecksExpiryInFinalWriteTransaction(t *testing.T) {
	fixture, mapped, _ := queryMappedTagFixture(t)
	reads := 0
	fixture.service.now = func() time.Time {
		reads++
		if reads == 1 {
			return time.UnixMilli(mapped.ExpiresAtMS - 1)
		}
		return time.UnixMilli(mapped.ExpiresAtMS)
	}
	result, err := fixture.service.StartImport(fixture.context, mapped.ID, mapped.Version)
	if result.ID != "" || !errors.Is(err, ErrExpired) {
		t.Fatalf("expired during preflight: result=%#v error=%v clockReads=%d", result, err, reads)
	}
	assertNextStartUnchanged(t, fixture, mapped)
}

func TestStartRejectsIneligibleFrozenTarget(t *testing.T) {
	tests := []struct{ name, mutate string }{
		{"disabled platform", `UPDATE platforms SET enabled=0 WHERE id=(SELECT target_platform_id FROM emulationstation_import_collections WHERE import_id=? LIMIT 1)`},
		{"disabled core", `UPDATE cores SET enabled=0 WHERE id=(SELECT target_default_core_id FROM emulationstation_import_collections WHERE import_id=? LIMIT 1)`},
		{"missing platform binding", `DELETE FROM runtime_binding_platforms WHERE platform_id=(SELECT target_platform_id FROM emulationstation_import_collections WHERE import_id=? LIMIT 1)`},
		{"missing target binding", `DELETE FROM runtime_target_bindings WHERE core_id=(SELECT target_default_core_id FROM emulationstation_import_collections WHERE import_id=? LIMIT 1)`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, mapped, _ := queryMappedTagFixture(t)
			mustExecEmulationStationTest(t, fixture.database, test.mutate, mapped.ID)
			result, err := fixture.service.StartImport(fixture.context, mapped.ID, mapped.Version)
			if result.ID != "" || !errors.Is(err, ErrMappingTargetChanged) {
				t.Fatalf("%s queued result=%#v error=%v", test.name, result, err)
			}
			assertNextStartUnchanged(t, fixture, mapped)
		})
	}
}

func assertNextStartUnchanged(t *testing.T, fixture lifecycleFixture, mapped Summary) {
	t.Helper()
	current, err := fixture.service.Get(fixture.context, mapped.ID)
	if err != nil || current.State != mapped.State || current.Version != mapped.Version || current.ImportJobID != nil {
		t.Fatalf("failed start changed plan=%#v error=%v", current, err)
	}
	var count int
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT count(*) FROM jobs WHERE scope_type='EMULATIONSTATION_IMPORT' AND scope_id=? AND kind='SERVER_EMULATIONSTATION_IMPORT'`, mapped.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed start created %d jobs", count)
	}
}

func TestStartPreservesRootSnapshotStorageFailure(t *testing.T) {
	fixture, mapped, _ := queryMappedTagFixture(t)
	mustExecEmulationStationTest(t, fixture.database, `ALTER TABLE emulationstation_imports RENAME COLUMN root_config_digest TO broken_root_digest`)
	result, err := fixture.service.StartImport(fixture.context, mapped.ID, mapped.Version)
	if result.ID != "" || err == nil || errors.Is(err, ErrSourceChanged) || !strings.Contains(err.Error(), "root_config_digest") {
		t.Fatalf("root storage cause lost: result=%#v error=%v", result, err)
	}
}

func TestStartRecordsActingUserAudit(t *testing.T) {
	fixture, mapped, _ := queryMappedTagFixture(t)
	const editor = "01980000-0000-7000-8000-000000000843"
	mustExecEmulationStationTest(t, fixture.database, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('es-start-editor-profile','Editor',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'es-start-editor-profile','es-start-editor','Editor','ADMIN','ENABLED',1,1)`, editor)
	ctx := authn.WithPrincipal(fixture.context, authn.Principal{UserID: editor, ProfileID: "es-start-editor-profile", Role: "ADMIN"})
	if _, err := fixture.service.StartImport(ctx, mapped.ID, mapped.Version); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT count(*) FROM audit_events WHERE resource_id=? AND action='EMULATIONSTATION_IMPORT_STARTED' AND actor_user_id=?`, mapped.ID, editor).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("started import has %d acting-user audit records", count)
	}
}
