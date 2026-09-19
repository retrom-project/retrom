package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

var errMappingStep = errors.New("mapping step failed")

func TestMappingsRollbackEveryProjectionAtEachWriteBoundary(t *testing.T) {
	t.Parallel()
	db := mappingDatabase(t)
	seedSecondMappingCollection(t, db)
	before := planRows(t, db)
	faultDB := testsupport.OpenSQLFaultDatabase(
		t, db, testsupport.SQLFaultHooks{
			BeforeExec: func(
				_ context.Context, query string,
				_ []driver.NamedValue,
			) error {
				if strings.TrimSpace(query) == "COMMIT" {
					return errMappingStep
				}
				return nil
			},
		},
	)
	repo := NewMappings(faultDB)
	service := emulationstationimportservice.NewMappings(repo, func() time.Time { return time.UnixMilli(10) })
	result, err := service.Update(t.Context(), "import-0", 1, []emulationstationimportmodel.Mapping{
		{CollectionID: mappingCollection, Action: "SKIP", TagIDs: []string{}},
		{CollectionID: secondMappingCollection, Action: "SKIP", TagIDs: []string{}},
	}, mappingActor)
	if err == nil || result.ID != "" {
		t.Fatalf("pre-commit failure returned success or partial result: %#v error=%v", result, err)
	}
	if !reflect.DeepEqual(planRows(t, db), before) {
		t.Fatal("failure changed persisted plan, collections, tags or immutable evidence")
	}
}
