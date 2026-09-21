package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"strings"
	"sync/atomic"

	"retrom/internal/testsupport"
)

func recoveryReviewHooks(stage, itemID string, hook func(context.Context, string, []driver.NamedValue) error, hits *atomic.Int64) testsupport.SQLFaultHooks {
	return testsupport.SQLFaultHooks{BeforeExec: hook, BeforeQuery: hook, AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
		if stage != "affected rows" && stage != "zero rows" {
			return result, nil
		}
		if !strings.HasPrefix(strings.Join(strings.Fields(query), " "), "UPDATE emulationstation_import_items SET execution_state='REVIEW_PENDING'") {
			return result, nil
		}
		for _, arg := range args {
			if arg.Value == itemID {
				hits.Add(1)
				return recoveryReviewResult{Result: result, zero: stage == "zero rows"}, nil
			}
		}
		return result, nil
	}}
}

type recoveryReviewResult struct {
	driver.Result
	zero bool
}

func (result recoveryReviewResult) RowsAffected() (int64, error) {
	if result.zero {
		return 0, nil
	}
	return 0, errExecutionReviewFault
}
