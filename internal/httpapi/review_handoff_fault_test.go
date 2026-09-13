package httpapi

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
)

var errReviewAttachFault = errors.New("injected EmulationStation review attachment failure")

type reviewAttachFault struct {
	database *sql.DB
	enabled  atomic.Bool
	hits     atomic.Int64
}

func (fault *reviewAttachFault) beforeExec(ctx context.Context, query string, args []driver.NamedValue) error {
	const handoff = "UPDATE emulationstation_import_items SET execution_state='REVIEW_PENDING',library_import_job_id=?,library_import_item_id=?, warnings_json=?,error_code=NULL,error_details_json=NULL,retryable=0,completed_at_ms=?, updated_at_ms=?,version=version+1 WHERE id=? AND import_id=? AND version=? AND execution_state='VALIDATING' AND library_import_job_id IS ? AND library_import_item_id IS ? AND metadata_json=? AND warnings_json=? AND EXISTS(SELECT 1 FROM import_items WHERE id=? AND import_job_id=? AND state='REVIEW_PENDING')"
	if !fault.enabled.Load() || strings.Join(strings.Fields(query), " ") != handoff || len(args) != 14 {
		return nil
	}
	jobID, jobOK := args[0].Value.(string)
	itemID, itemOK := args[1].Value.(string)
	sourceID, sourceOK := args[5].Value.(string)
	if !jobOK || !itemOK || !sourceOK || jobID == "" || itemID == "" || sourceID == "" {
		return nil
	}
	var reserved bool
	if err := fault.database.QueryRowContext(ctx, `SELECT source.execution_state='VALIDATING' AND source.library_import_item_id=? AND source.library_import_job_id=?
AND EXISTS(SELECT 1 FROM import_items item WHERE item.id=? AND item.import_job_id=?
AND item.review_handoff_kind='EMULATIONSTATION' AND item.state='REVIEW_PENDING')
FROM emulationstation_import_items source WHERE source.id=?`, itemID, jobID, itemID, jobID, sourceID).Scan(&reserved); err != nil {
		return fmt.Errorf("read injected review reservation boundary: %w", err)
	}
	if !reserved {
		return nil
	}
	fault.hits.Add(1)
	return errReviewAttachFault
}
