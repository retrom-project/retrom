package emulationstationimport

import (
	"errors"
	"reflect"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

var errMappingStep = errors.New("mapping step failed")

func TestMappingsRollbackEveryProjectionAtEachWriteBoundary(t *testing.T) {
	t.Parallel()
	db := mappingDatabase(t)
	seedSecondMappingCollection(t, db)
	before := planRows(t, db)
	repo := NewMappings(db, WithMappingsPreCommitHook(func(dbexec.Executor) error { return errMappingStep }))
	service := emulationstationimportservice.NewMappings(repo, func() time.Time { return time.UnixMilli(10) })
	result, err := service.Update(t.Context(), "import-0", 1, []emulationstationimportmodel.Mapping{
		{CollectionID: mappingCollection, Action: "SKIP", TagIDs: []string{}},
		{CollectionID: secondMappingCollection, Action: "SKIP", TagIDs: []string{}},
	}, mappingActor)
	if err == nil || result.ID != "" {
		t.Fatalf("pre-commit failure returned success or partial result: %#v error=%v", result, err)
	}
	if !errors.Is(err, errMappingStep) {
		t.Fatalf("pre-commit cause lost: %v", err)
	}
	if !reflect.DeepEqual(planRows(t, db), before) {
		t.Fatal("failure changed persisted plan, collections, tags or immutable evidence")
	}
}
