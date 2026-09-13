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

	"retrom/internal/adapter/integration/libraryimport"
	"retrom/internal/testkit/testsupport"
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
	fixture.service.importer = libraryimport.New(fixture.service.database, fixture.service.now).WithBlobStore(
		fixture.service.blobs,
	)
	return fault
}

func (fault *workingStateFault) beforeExec(ctx context.Context, query string, args []driver.NamedValue) error {
	kind := workingExitKind(query, args, fault.itemID)
	if kind == "" {
		return nil
	}
	var state string
	if err := fault.database.QueryRowContext(ctx, `SELECT execution_state FROM emulationstation_import_items WHERE id=?`, fault.itemID).Scan(

		&state,
	); err != nil {
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
	case "UPDATE emulationstation_import_items SET execution_state='VALIDATING',library_import_job_id=?,library_import_item_id=?, version=version+1,updated_at_ms=? WHERE id=? AND import_id=? AND version=? AND execution_state='COPYING' AND library_import_job_id IS NULL AND library_import_item_id IS NULL AND EXISTS(SELECT 1 FROM server_import_upload_owners owner JOIN import_jobs imported ON imported.upload_session_id=owner.upload_session_id JOIN import_items item ON item.import_job_id=imported.id WHERE owner.kind='EMULATIONSTATION' AND owner.source_item_id=emulationstation_import_items.id AND owner.upload_session_id=? AND imported.id=? AND item.id=? AND item.review_handoff_kind='EMULATIONSTATION')":
		if len(
			args,
		) == 9 && args[3].Value == itemID && args[0].Value == args[7].Value && args[1].Value == args[8].Value && args[0].Value != nil && args[1].Value != nil {
			return "attachment"
		}
	case "UPDATE emulationstation_import_items SET execution_state=?,error_code=?,retryable=?,error_details_json=?,existing_game_id=COALESCE(?,existing_game_id), existing_matches_json=COALESCE(?,existing_matches_json),completed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=? AND import_id=? AND version=? AND execution_state=? AND metadata_json=? AND content_kind=? AND library_import_job_id IS ? AND library_import_item_id IS ? AND execution_state IN ('COPYING','VALIDATING')":
		if len(args) == 16 && args[8].Value == itemID && args[11].Value == "COPYING" && args[0].Value != "COPYING" {
			return "terminal"
		}
	}
	return ""
}
