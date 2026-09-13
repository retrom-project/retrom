package emulationstationimport

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	tagrepository "retrom/internal/persistence/tagging"
	application "retrom/internal/service/emulationstationimport"
	"retrom/internal/service/tagging"
)

const secondMappingCollection = "019b0000-0000-7000-8000-000000000004"

func seedSecondMappingCollection(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO emulationstation_import_gamelists(import_id,relative_path,size_bytes,source_facts_digest,content_digest,parse_state,game_count,created_at_ms) SELECT import_id,'nested/gamelist.xml',size_bytes,source_facts_digest,content_digest,parse_state,game_count,created_at_ms FROM emulationstation_import_gamelists WHERE import_id='import-0'`,
		`INSERT INTO emulationstation_import_collections(id,import_id,gamelist_relative_path,relative_directory,display_name,game_count,created_at_ms,updated_at_ms) SELECT '` + secondMappingCollection + `',import_id,'nested/gamelist.xml','nested','Second',1,1,1 FROM emulationstation_import_collections WHERE id='` + mappingCollection + `'`,
		`UPDATE emulationstation_imports SET gamelist_count=2,collection_count=2,game_count=2,processable_item_count=2 WHERE id='import-0'`,
	} {
		if _, err := db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMappingsPreserveExistingTagAssignmentOnDomainRejection(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"missing tag", "too many tags", "duplicate tag", "empty collection", "foreign collection"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			db := mappingDatabase(t)
			instance := seedMappingTarget(t, db)
			mapping, want := rejectedMapping(t, db, instance, kind)
			before := planRows(t, db)
			service := application.NewMappings(NewMappings(db), tagging.New(tagrepository.New(db), time.Now), func() time.Time { return time.UnixMilli(10) })
			result, err := service.Update(t.Context(), "import-0", 1, []application.Mapping{mapping}, mappingActor)
			if result.ID != "" || !errors.Is(err, want) {
				t.Fatalf("%s result=%#v error=%v want=%v", kind, result, err, want)
			}
			if !reflect.DeepEqual(planRows(t, db), before) {
				t.Fatal("invalid selection changed persisted plan or tags")
			}
		})
	}
}

func rejectedMapping(t *testing.T, db *sql.DB, instance, kind string) (application.Mapping, error) {
	t.Helper()
	mapping := application.Mapping{CollectionID: mappingCollection, Action: "IMPORT", PlatformInstanceID: instance, TagIDs: []string{mappingTag}}
	want := application.ErrInvalid
	switch kind {
	case "missing tag":
		mapping.TagIDs = []string{secondMappingCollection}
		want = tagging.ErrReferenceInvalid
	case "too many tags":
		mapping.TagIDs = make([]string, 21)
		want = tagging.ErrAssignmentLimitExceeded
	case "duplicate tag":
		mapping.TagIDs = []string{mappingTag, mappingTag}
	case "empty collection":
		if _, err := db.ExecContext(t.Context(), `UPDATE emulationstation_import_collections SET game_count=0 WHERE id=?`, mappingCollection); err != nil {
			t.Fatal(err)
		}
	case "foreign collection":
		if err := NewCreation(db).WithCreate(t.Context(), func(writer application.CreationWriter) error {
			_, err := writer.Insert(t.Context(), creationPlan(1))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(t.Context(), `INSERT INTO emulationstation_import_gamelists(import_id,relative_path,size_bytes,source_facts_digest,content_digest,parse_state,game_count,created_at_ms) SELECT 'import-1',relative_path,size_bytes,source_facts_digest,content_digest,parse_state,game_count,created_at_ms FROM emulationstation_import_gamelists WHERE import_id='import-0'`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(t.Context(), `UPDATE emulationstation_import_collections SET import_id='import-1' WHERE id=?`, mappingCollection); err != nil {
			t.Fatal(err)
		}
	}
	return mapping, want
}

type mappingAffectedResult struct {
	count int64
	err   error
}

func (result mappingAffectedResult) LastInsertId() (int64, error) { return 0, nil }
func (result mappingAffectedResult) RowsAffected() (int64, error) { return result.count, result.err }

func TestMappingsPreserveAffectedRowAndStorageErrors(t *testing.T) {
	t.Parallel()
	cause := errors.New("driver affected rows failed")
	cases := []struct {
		result           sql.Result
		storageErr, want error
	}{
		{nil, cause, cause},
		{mappingAffectedResult{err: cause}, nil, cause},
		{mappingAffectedResult{count: 0}, nil, application.ErrVersionConflict},
		{mappingAffectedResult{count: 2}, nil, application.ErrVersionConflict},
	}
	for _, test := range cases {
		err := requireMappingChange(test.result, test.storageErr, application.ErrVersionConflict)
		if !errors.Is(err, test.want) {
			t.Fatalf("mapping write cause=%v want=%v", err, test.want)
		}
	}
}
