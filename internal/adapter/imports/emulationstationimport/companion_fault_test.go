package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"retrom/internal/testkit/testsupport"
)

var errCompanionFault = errors.New("companion storage fault")

type companionFaultResult struct {
	driver.Result
	zero bool
}

func (result companionFaultResult) RowsAffected() (int64, error) {
	if result.zero {
		return 0, nil
	}
	return 0, errCompanionFault
}

func TestESCompanionWriteAndAffectedFailuresRollback(t *testing.T) {
	for _, stage := range []string{"job", "item", "mapping", "candidate", "blob"} {
		for _, failure := range []string{"write", "count", "zero"} {
			if stage == "blob" && failure != "write" {
				continue
			}
			t.Run(stage+"/"+failure, func(t *testing.T) { testCompanionWriteFault(t, stage, failure) })
		}
	}
}

func testCompanionWriteFault(t *testing.T, stage, failure string) {
	t.Helper()
	fixture, unit, owner, file, blob := companionRecordFixture(t)
	before := materialAuthoritySnapshot(t, fixture, owner.Before.Item.ID)
	hits := 0
	match := func(query string, args []driver.NamedValue) bool {
		prefixes := map[string]string{
			"job":       "UPDATE jobs SET version=version",
			"item":      "UPDATE emulationstation_import_items SET version=version",
			"mapping":   "UPDATE emulationstation_import_collections SET updated_at_ms=updated_at_ms",
			"candidate": "UPDATE emulationstation_import_item_files SET ordinal=ordinal",
			"blob":      "INSERT INTO blobs(id,",
		}
		ids := map[string]string{
			"job":       unit.JobID,
			"item":      owner.Before.Item.ID,
			"candidate": file.ItemID,
			"blob":      blob.SHA256,
		}
		if !strings.HasPrefix(strings.TrimSpace(query), prefixes[stage]) {
			return false
		}
		target := ids[stage]
		if stage == "mapping" {
			target = file.CollectionID
		}
		for _, arg := range args {
			if arg.Value == target {
				return true
			}
		}
		return false
	}
	fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if failure == "write" && match(query, args) {
				hits++
				return errCompanionFault
			}
			return nil
		},
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if failure != "write" && match(query, args) {
				hits++
				return companionFaultResult{Result: result, zero: failure == "zero"}, nil
			}
			return result, nil
		},
	})
	id, err := fixture.service.companions().Record(fixture.context, unit, owner, file, blob)
	cause := errCompanionFault
	if failure == "zero" {
		cause = ErrVersionConflict
	}
	if !errors.Is(err, cause) || hits != 1 || id != "" {
		t.Fatalf("id=%s error=%v hits=%d", id, err, hits)
	}
	if before != materialAuthoritySnapshot(t, fixture, owner.Before.Item.ID) {
		t.Fatal("failed write retained catalog or binding")
	}
}
