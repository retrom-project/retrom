package emulationstationimport

import (
	"errors"
	"reflect"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	tagrepository "retrom/internal/repo/tagging"
	application "retrom/internal/service/emulationstationimport"
	"retrom/internal/service/tagging"
)

func TestMappingsRejectEveryIneligibleTargetBeforeTagChanges(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, mutate string }{
		{"deleted instance", `UPDATE platform_instances SET deleted_at_ms=2 WHERE id=?`},
		{"disabled platform", `UPDATE platforms SET enabled=0 WHERE id=(SELECT platform_id FROM platform_instances WHERE id=?)`},
		{"disabled core", `UPDATE cores SET enabled=0 WHERE id=(SELECT default_core_id FROM platform_instances WHERE id=?)`},
		{"disabled binding", `UPDATE runtime_target_bindings SET launch_policy='DISABLED' WHERE core_id=(SELECT default_core_id FROM platform_instances WHERE id=?)`},
		{"missing platform binding", `DELETE FROM runtime_binding_platforms WHERE platform_id=(SELECT platform_id FROM platform_instances WHERE id=?)`},
		{"missing target binding", `DELETE FROM runtime_target_bindings WHERE core_id=(SELECT default_core_id FROM platform_instances WHERE id=?)`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			db := mappingDatabase(t)
			instance := seedMappingTarget(t, db)
			if _, err := db.ExecContext(t.Context(), test.mutate, instance); err != nil {
				t.Fatal(err)
			}
			before := planRows(t, db)
			service := application.NewMappings(NewMappings(db), tagging.New(tagrepository.New(db), time.Now), func() time.Time { return time.UnixMilli(10) })
			result, err := service.Update(t.Context(), "import-0", 1, []emulationstationimportmodel.Mapping{{CollectionID: mappingCollection, Action: "IMPORT", PlatformInstanceID: instance, TagIDs: []string{}}}, mappingActor)
			if result.ID != "" || !errors.Is(err, emulationstationimportmodel.ErrInvalid) {
				t.Fatalf("ineligible %s result=%#v error=%v", test.name, result, err)
			}
			if !reflect.DeepEqual(planRows(t, db), before) {
				t.Fatal("ineligible target changed plan or tags")
			}
		})
	}
}

func TestMappingsFreezeSelectedDATAndCurrentTagName(t *testing.T) {
	t.Parallel()
	db := mappingDatabase(t)
	instance := seedMappingTarget(t, db)
	const dat = "mapping-active-dat"
	if _, err := db.ExecContext(t.Context(), `INSERT INTO dat_versions(id,core_id,provider_id,target_id,builtin_relative_path,sha256,parser_version,parse_status,is_active,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
SELECT ?,instance.default_core_id,binding.provider_id,binding.target_id,'mapping-fixture.dat',?,'mapping-test','READY',1,1,1,1,1
FROM platform_instances instance JOIN runtime_target_bindings binding ON binding.core_id=instance.default_core_id WHERE instance.id=?`, dat, planDigest, instance); err != nil {
		t.Fatal(err)
	}
	tags := tagging.New(tagrepository.New(db), func() time.Time { return time.UnixMilli(5) })
	if _, err := tags.Rename(t.Context(), mappingActor, mappingTag, "Renamed", 1); err != nil {
		t.Fatal(err)
	}
	service := application.NewMappings(NewMappings(db), tags, func() time.Time { return time.UnixMilli(10) })
	_, err := service.Update(t.Context(), "import-0", 1, []emulationstationimportmodel.Mapping{{CollectionID: mappingCollection, Action: "IMPORT", PlatformInstanceID: instance, TagIDs: []string{mappingTag}}}, mappingActor)
	if err != nil {
		t.Fatal(err)
	}
	var frozenDAT, frozenTagName string
	var tagVersion int64
	if err := db.QueryRowContext(t.Context(), `SELECT target_dat_version_id,json_extract(tag_snapshot_json,'$[0].name'),(SELECT version FROM tags WHERE id=?) FROM emulationstation_import_collections WHERE id=?`, mappingTag, mappingCollection).Scan(&frozenDAT, &frozenTagName, &tagVersion); err != nil {
		t.Fatal(err)
	}
	if frozenDAT != dat || frozenTagName != "Renamed" || tagVersion != 2 {
		t.Fatalf("snapshot DAT=%s tag=%s version=%d", frozenDAT, frozenTagName, tagVersion)
	}
}
