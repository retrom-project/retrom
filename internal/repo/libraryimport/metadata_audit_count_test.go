package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"

	libraryimportmodel "retrom/internal/model/libraryimport"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/testkit/testsupport"
)

func TestMetadataAuditZeroReturnedKeysRollsBackDraftSearchAndEvent(t *testing.T) {
	db := metadataDatabase(t)
	before := readMetadataState(t, db)
	precedingWrites, auditWrites := 0, 0
	intercepted := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, _ []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(query, "UPDATE review_drafts SET") || strings.HasPrefix(query, "UPDATE import_items SET") {
				precedingWrites++
			}
			return result, nil
		},
		AfterQuery: func(_ context.Context, query string, args []driver.NamedValue, rows driver.Rows) (driver.Rows, error) {
			if !strings.HasPrefix(query, "INSERT INTO review_events(") || len(args) < 2 || args[1].Value != "item" {
				return rows, nil
			}
			values := make([]driver.Value, len(rows.Columns()))
			// RETURNING is lazy: advancing the real cursor proves the audit INSERT ran.
			if err := rows.Next(values); err != nil {
				return nil, err
			}
			if len(values) != 1 || values[0] != args[0].Value {
				return nil, errUnexpectedMetadataAuditKey
			}
			auditWrites++
			return metadataHiddenAuditKeys{Rows: rows}, nil
		},
	})
	service := application.NewMetadataSeeder(NewMetadata(intercepted), metadataNow)
	version, warnings, err := service.Seed(t.Context(), "item", libraryimportmodel.ServerMetadata{Title: "Changed"}, 2027)
	if !errors.Is(err, libraryimportmodel.ErrVersionConflict) || version != 0 || warnings != nil || precedingWrites != 2 || auditWrites != 1 {
		t.Fatalf("version=%d warnings=%+v preceding=%d audit=%d state=%+v err=%v", version, warnings, precedingWrites, auditWrites, readMetadataState(t, db), err)
	}
	if after := readMetadataState(t, db); after != before {
		t.Fatalf("partial metadata committed: before=%+v after=%+v", before, after)
	}
}

var errUnexpectedMetadataAuditKey = errors.New("unexpected metadata audit returned key")

type metadataHiddenAuditKeys struct{ driver.Rows }

func (metadataHiddenAuditKeys) Next([]driver.Value) error { return io.EOF }
