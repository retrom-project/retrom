package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	libraryimportmodel "retrom/internal/model/libraryimport"
	"retrom/internal/testkit/testsupport"
)

type handoffHiddenAuditKeys struct{ driver.Rows }

func (handoffHiddenAuditKeys) Next([]driver.Value) error { return io.EOF }

func TestESReviewHandoffAuditZeroKeysRollsBackRealInsert(t *testing.T) {
	fixture := newLifecycleFixture(t)
	_, unit := startLifecycleImport(t, fixture, "", "nes")
	item, ordinary := reserveExecutionReview(t, fixture, unit)
	before := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID)
	var hits atomic.Int64
	faultDB := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		AfterQuery: func(_ context.Context, query string, args []driver.NamedValue, rows driver.Rows) (driver.Rows, error) {
			if !strings.HasPrefix(strings.Join(strings.Fields(query), " "), "INSERT INTO review_events(") || len(args) < 2 || args[1].Value != ordinary.Items[0].ItemID {
				return rows, nil
			}
			values := make([]driver.Value, len(rows.Columns()))
			if err := rows.Next(values); err != nil {
				return nil, err
			}
			if len(values) != 1 || values[0] != args[0].Value {
				return nil, errExecutionReviewFault
			}
			hits.Add(1)
			return handoffHiddenAuditKeys{Rows: rows}, nil
		},
	})
	fixture.service.database = faultDB
	err := fixture.service.finalizeReviewHandoff(fixture.context, unit, item, ordinary.Created.ImportJobID, ordinary.Items[0].ItemID, nil)
	if !errors.Is(err, libraryimportmodel.ErrVersionConflict) || hits.Load() != 1 {
		t.Fatalf("audit cause=%v hits=%d", err, hits.Load())
	}
	if after := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID); after != before {
		t.Fatal("zero audit keys retained metadata/source changes")
	}
}
