package emulationstationimport

import (
	"errors"
	"strings"
	"testing"

	"retrom/internal/authn"
)

func TestMappingPreservesDatabaseFailureCause(t *testing.T) {
	tests := []struct{ name, alter, column string }{
		{"plan", `ALTER TABLE emulationstation_imports RENAME COLUMN state TO broken_state`, "state"},
		{"target", `ALTER TABLE runtime_binding_platforms RENAME COLUMN platform_id TO broken_platform_id`, "platform_id"},
		{"tags", `ALTER TABLE tags RENAME COLUMN status TO broken_status`, "status"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, mapped, _ := queryMappedTagFixture(t)
			mapping := nextMapping(t, fixture, mapped)
			mustExecEmulationStationTest(t, fixture.database, test.alter)
			result, err := fixture.service.UpdateMappings(fixture.context, mapped.ID, mapped.Version, []Mapping{mapping})
			if result.ID != "" || err == nil || errors.Is(err, ErrInvalid) || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), test.column) {
				t.Fatalf("database %s cause lost: result=%#v error=%v", test.name, result, err)
			}
		})
	}
}

func nextMapping(t *testing.T, fixture lifecycleFixture, mapped Summary) Mapping {
	t.Helper()
	mapping := Mapping{Action: "IMPORT", TagIDs: []string{}}
	err := fixture.database.QueryRowContext(fixture.context, `SELECT id,target_platform_instance_id FROM emulationstation_import_collections WHERE import_id=? AND mapping_action='IMPORT' LIMIT 1`, mapped.ID).Scan(&mapping.CollectionID, &mapping.PlatformInstanceID)
	if err != nil {
		t.Fatal(err)
	}
	return mapping
}

func TestMappingsRecordActingPrincipalOnTagChanges(t *testing.T) {
	fixture, mapped, tag := queryMappedTagFixture(t)
	const actor = "01980000-0000-7000-8000-000000000849"
	mustExecEmulationStationTest(t, fixture.database, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('mapping-editor-profile','Mapping editor',1)`)
	mustExecEmulationStationTest(t, fixture.database, `INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms) VALUES(?,'mapping-editor-profile','mapping-editor','Mapping editor','ADMIN','ENABLED',1,1)`, actor)
	mapping := nextMapping(t, fixture, mapped)
	mapping.Action = "SKIP"
	mapping.PlatformInstanceID = ""
	ctx := authn.WithPrincipal(fixture.context, authn.Principal{UserID: actor, Role: "ADMIN"})
	result, err := fixture.service.UpdateMappings(ctx, mapped.ID, mapped.Version, []Mapping{mapping})
	if err != nil {
		t.Fatal(err)
	}
	var actualActor string
	var actualVersion int64
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT updated_by_user_id,version FROM tags WHERE id=?`, tag.TagID).Scan(&actualActor, &actualVersion); err != nil {
		t.Fatal(err)
	}
	if result.Version != mapped.Version+1 || actualActor != actor || actualVersion != tag.Version+1 {
		t.Fatalf("mapping actor=%s version=%d result=%#v", actualActor, actualVersion, result)
	}
}
