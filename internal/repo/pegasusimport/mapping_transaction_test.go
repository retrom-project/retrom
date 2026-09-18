package pegasusimport

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"retrom/internal/adapter/runtime/dependencies"
	pegasusimportmodel "retrom/internal/model/pegasusimport"
	taggingmodel "retrom/internal/model/tagging"
	pegasusimportservice "retrom/internal/service/pegasusimport"
	"retrom/internal/testkit/testsupport"
)

const (
	mappingActor      = "019b0000-0000-7000-8000-000000000001"
	mappingCollection = "019b0000-0000-7000-8000-000000000002"
	mappingTag        = "019b0000-0000-7000-8000-000000000003"
)

func mappingDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := creationDatabase(t)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('mapping-profile','Mapping',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'mapping-profile','mapping-admin','Mapping Admin','ADMIN','ENABLED',1,1)`, mappingActor); err != nil {
		t.Fatal(err)
	}
	plan := creationPlan(0)
	plan.ActorID = mappingActor
	if _, err := NewCreation(db).CommitCreation(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE pegasus_imports SET state='AWAITING_MAPPING',phase=NULL,collection_count=1 WHERE id='import-0'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO pegasus_import_collections(id,import_id,metadata_relative_path,segment_ordinal,name,game_count,created_at_ms,updated_at_ms)
VALUES(?,'import-0','metadata.pegasus.txt',0,'Collection',0,1,1)`, mappingCollection); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO tags(id,name,name_key,search_text,status,created_by_user_id,updated_by_user_id,created_at_ms,updated_at_ms)
VALUES(?,'Tag','tag','tag','ACTIVE',?,?,1,1)`, mappingTag, mappingActor, mappingActor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO pegasus_collection_tags(collection_id,tag_id,assigned_by_user_id,created_at_ms)
VALUES(?,?,?,1)`, mappingCollection, mappingTag, mappingActor); err != nil {
		t.Fatal(err)
	}
	return db
}

func mappingRows(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, table := range []string{"pegasus_imports", "pegasus_import_collections", "pegasus_collection_tags", "tags", "audit_events"} {
		result[table] = workflowTable(t, db, table)
	}
	return result
}

type failingMappingCommit struct {
	repository *Mappings
	cause      error
}

func (r failingMappingCommit) WithMappings(ctx context.Context, work func(pegasusimportmodel.MappingScope) error) error {
	return r.repository.WithMappings(ctx, func(scope pegasusimportmodel.MappingScope) error {
		if err := work(scope); err != nil {
			return err
		}
		return r.cause
	})
}

func TestMappingsRollBackRelationsAndTagVersionsOnLateFailure(t *testing.T) {
	t.Parallel()
	db := mappingDatabase(t)
	before := mappingRows(t, db)
	cause := errors.New("late mapping failure")
	service := pegasusimportservice.NewMappings(failingMappingCommit{repository: NewMappings(db), cause: cause}, func() time.Time { return time.UnixMilli(10) })
	value, err := service.Update(t.Context(), "import-0", 1, []pegasusimportmodel.Mapping{{CollectionID: mappingCollection, Action: "SKIP", TagIDs: []string{}}}, mappingActor)
	if !errors.Is(err, cause) || value.ID != "" {
		t.Fatalf("late mapping failure: %#v %v", value, err)
	}
	if !reflect.DeepEqual(mappingRows(t, db), before) {
		t.Fatal("mapping rollback left collection or tag writes")
	}
}

func TestMappingsRejectStalePlanAfterUpdatingCollection(t *testing.T) {
	t.Parallel()
	db := mappingDatabase(t)
	beforeRows := mappingRows(t, db)
	err := NewMappings(db).WithMappings(t.Context(), func(scope pegasusimportmodel.MappingScope) error {
		before, err := scope.Read.Import(t.Context(), "import-0")
		if err != nil {
			return err
		}
		owner := taggingmodel.Owner{Kind: taggingmodel.OwnerPegasusCollection, ID: mappingCollection}
		_, refs, err := scope.Tags.ReplaceOwnerReferences(t.Context(), owner, []string{}, mappingActor, 10)
		if err != nil {
			return err
		}
		if err := scope.Write.Put(t.Context(), pegasusimportmodel.CollectionMapping{ImportID: "import-0", Mapping: pegasusimportmodel.Mapping{CollectionID: mappingCollection, Action: "SKIP"}, Tags: refs, NowMS: 10}); err != nil {
			return err
		}
		before.Version++
		return scope.Write.Advance(t.Context(), pegasusimportmodel.MappingAdvance{Before: before, NowMS: 10})
	})
	if !errors.Is(err, pegasusimportmodel.ErrVersionConflict) {
		t.Fatalf("stale mapping committed: %v", err)
	}
	if !reflect.DeepEqual(mappingRows(t, db), beforeRows) {
		t.Fatal("stale mapping changed collections or tags")
	}
}

func TestMappingsPersistSelectionThenClearItWhenSkipped(t *testing.T) {
	t.Parallel()
	db := mappingDatabase(t)
	instance := seedMappingTarget(t, db)
	service := pegasusimportservice.NewMappings(NewMappings(db), func() time.Time { return time.UnixMilli(10) })
	value, err := service.Update(t.Context(), "import-0", 1, []pegasusimportmodel.Mapping{{CollectionID: mappingCollection, Action: "IMPORT", PlatformInstanceID: instance, TagIDs: []string{mappingTag}}}, mappingActor)
	if err != nil {
		t.Fatal(err)
	}
	if value.Version != 2 || value.MappingVersion != 2 || value.Counts.MappedCollections != 1 {
		t.Fatalf("import mapping: %#v", value)
	}
	assertStoredMappingTarget(t, db, instance)
	value, err = service.Update(t.Context(), "import-0", 2, []pegasusimportmodel.Mapping{{CollectionID: mappingCollection, Action: "SKIP", TagIDs: []string{}}}, mappingActor)
	if err != nil {
		t.Fatal(err)
	}
	if value.Version != 3 || value.MappingVersion != 3 || value.Counts.MappedCollections != 0 || value.Counts.SkippedCollections != 1 {
		t.Fatalf("skip mapping: %#v", value)
	}
	var cleared bool
	var tags, tagVersion int
	if err := db.QueryRowContext(t.Context(), `SELECT mapping_action='SKIP' AND tag_snapshot_json='[]' AND target_platform_instance_id IS NULL
AND target_platform_instance_version IS NULL AND target_platform_id IS NULL AND target_default_core_id IS NULL
AND target_provider_id IS NULL AND target_id IS NULL AND target_dat_version_id IS NULL,
(SELECT count(*) FROM pegasus_collection_tags),(SELECT version FROM tags)
FROM pegasus_import_collections WHERE id=?`, mappingCollection).Scan(&cleared, &tags, &tagVersion); err != nil {
		t.Fatal(err)
	}
	if !cleared || tags != 0 || tagVersion != 2 {
		t.Fatalf("skip kept selection: cleared=%v tags=%d version=%d", cleared, tags, tagVersion)
	}
}

func seedMappingTarget(t *testing.T, db *sql.DB) string {
	t.Helper()
	deps, err := dependencies.Load(filepath.Join("..", "..", "..", "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := testsupport.SeedRuntimeProviders(t.Context(), db, deps.RuntimeCatalog); err != nil {
		t.Fatal(err)
	}
	if err := testsupport.SeedPlatformInstances(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := db.QueryRowContext(t.Context(), `SELECT id FROM platform_instances WHERE platform_id='gba' AND enabled=1 ORDER BY sort_order,id LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func assertStoredMappingTarget(t *testing.T, db *sql.DB, instance string) {
	t.Helper()
	var exact bool
	var tagID string
	if err := db.QueryRowContext(t.Context(), `SELECT collection.target_platform_instance_id=instance.id
AND collection.target_platform_instance_version=instance.version AND collection.target_platform_id=instance.platform_id
AND collection.target_default_core_id=instance.default_core_id
AND EXISTS(SELECT 1 FROM runtime_target_bindings binding WHERE binding.core_id=instance.default_core_id
AND binding.provider_id=collection.target_provider_id AND binding.target_id=collection.target_id),
json_extract(collection.tag_snapshot_json,'$[0].tagId')
FROM pegasus_import_collections collection JOIN platform_instances instance ON instance.id=?
WHERE collection.id=?`, instance, mappingCollection).Scan(&exact, &tagID); err != nil {
		t.Fatal(err)
	}
	if !exact || tagID != mappingTag {
		t.Fatalf("mapping target drift: exact=%v tag=%s", exact, tagID)
	}
}

func TestMappingsRejectDisabledTargetWithoutClearingTags(t *testing.T) {
	t.Parallel()
	db := mappingDatabase(t)
	instance := seedMappingTarget(t, db)
	if _, err := db.ExecContext(t.Context(), `UPDATE platform_instances SET enabled=0,version=version+1 WHERE id=?`, instance); err != nil {
		t.Fatal(err)
	}
	before := mappingRows(t, db)
	service := pegasusimportservice.NewMappings(NewMappings(db), func() time.Time { return time.UnixMilli(10) })
	value, err := service.Update(t.Context(), "import-0", 1, []pegasusimportmodel.Mapping{{CollectionID: mappingCollection, Action: "IMPORT", PlatformInstanceID: instance, TagIDs: []string{}}}, mappingActor)
	if !errors.Is(err, pegasusimportmodel.ErrInvalid) || value.ID != "" {
		t.Fatalf("disabled mapping: %#v %v", value, err)
	}
	if !reflect.DeepEqual(mappingRows(t, db), before) {
		t.Fatal("disabled target changed plan or tags")
	}
}
