//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"retrom/internal/testkit/testsupport"
)

type reviewDiscardFault struct {
	stage, itemID                    string
	cause                            error
	itemWrites, sourceWrites, faults int
}

func newReviewDiscardFault(t *testing.T, fixture deduplicateFixture, itemID, stage string) *reviewDiscardFault {
	t.Helper()
	fault := &reviewDiscardFault{stage: stage, itemID: itemID, cause: errors.New("late discard transaction failure")}
	fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeQuery: fault.beforeQuery, BeforeExec: fault.beforeExec, AfterExec: fault.afterExec,
	})
	return fault
}

func (fault *reviewDiscardFault) beforeQuery(_ context.Context, query string, _ []driver.NamedValue) error {
	if fault.stage == "event" && strings.Contains(query, "INSERT INTO review_events") {
		fault.faults++
		return fault.cause
	}
	return nil
}

func (fault *reviewDiscardFault) beforeExec(_ context.Context, query string, args []driver.NamedValue) error {
	ownerFailure := fault.stage == "owner aggregate" && strings.HasPrefix(query, "UPDATE pegasus_imports SET")
	payloadFailure := fault.stage == "payload event" && strings.Contains(query, "INSERT INTO job_events") && len(args) > 2 && args[2].Value == fault.itemID
	if ownerFailure || payloadFailure {
		fault.faults++
		return fault.cause
	}
	return nil
}

func (fault *reviewDiscardFault) afterExec(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if discardStatementItem(query, args) == fault.itemID {
		fault.itemWrites += int(count)
	}
	if strings.HasPrefix(query, "UPDATE pegasus_import_items SET execution_state=") {
		fault.sourceWrites += int(count)
	}
	return result, nil
}
