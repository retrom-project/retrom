package emulationstationimport

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/testsupport"
)

var errWorkingStateExit = errors.New("injected terminalization failure")

type workingStateFault struct {
	database       *sql.DB
	itemID         string
	attachmentHits atomic.Int64
	terminalHits   atomic.Int64
}

func blockWorkingStateExit(t *testing.T, fixture lifecycleFixture, importID string) *workingStateFault {
	t.Helper()
	fault := &workingStateFault{database: fixture.database}
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT id FROM emulationstation_import_items
WHERE import_id=? ORDER BY gamelist_relative_path,game_ordinal,id LIMIT 1`, importID).Scan(&fault.itemID); err != nil {
		t.Fatal(err)
	}
	fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeExec: fault.beforeExec,
	})
	return fault
}

func (fault *workingStateFault) beforeExec(ctx context.Context, query string, args []driver.NamedValue) error {
	kind := workingExitKind(query, args, fault.itemID)
	if kind == "" {
		return nil
	}
	var state string
	if err := fault.database.QueryRowContext(ctx, `SELECT execution_state FROM emulationstation_import_items WHERE id=?`, fault.itemID).Scan(&state); err != nil {
		return fmt.Errorf("read injected working-state boundary: %w", err)
	}
	if state != "COPYING" {
		return nil
	}
	if kind == "attachment" {
		fault.attachmentHits.Add(1)
	} else {
		fault.terminalHits.Add(1)
	}
	return errWorkingStateExit
}

func workingExitKind(query string, args []driver.NamedValue, itemID string) string {
	query = strings.Join(strings.Fields(query), " ")
	switch query {
	case "UPDATE emulationstation_import_items SET execution_state='VALIDATING',library_import_job_id=?,library_import_item_id=?,updated_at_ms=? WHERE id=? AND execution_state='COPYING'":
		if len(args) == 4 && args[3].Value == itemID && args[0].Value != nil && args[1].Value != nil {
			return "attachment"
		}
	case "UPDATE emulationstation_import_items SET execution_state=?,error_code=?,retryable=?, error_details_json=?, existing_game_id=COALESCE(?,existing_game_id), completed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=? AND execution_state IN ('COPYING','VALIDATING')":
		if len(args) == 8 && args[7].Value == itemID && args[0].Value != "COPYING" {
			return "terminal"
		}
	}
	return ""
}
