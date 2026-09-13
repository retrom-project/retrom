//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"retrom/internal/testkit/testsupport"
)

var errDeduplicateDiscard = errors.New("injected second discard failure")

type deduplicateDiscardFault struct {
	mutex           sync.Mutex
	firstID, lastID string
	successfulFirst int
	rejectedLast    int
}

func newDeduplicateDiscardFault(t *testing.T, fixture deduplicateFixture, copies ServerImportResult) *deduplicateDiscardFault {
	t.Helper()
	ids := []string{copies.Items[0].ItemID, copies.Items[1].ItemID}
	sort.Strings(ids)
	fault := &deduplicateDiscardFault{firstID: ids[0], lastID: ids[1]}
	fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeExec: fault.before,
		AfterExec:  fault.after,
	})
	return fault
}

func discardStatementItem(query string, args []driver.NamedValue) string {
	normalized := strings.Join(strings.Fields(query), " ")
	if !strings.HasPrefix(normalized, "UPDATE import_items SET ") ||
		!strings.Contains(normalized, "state='DISCARDED'") || !strings.Contains(normalized, "AND state='REVIEW_PENDING'") ||
		!strings.Contains(normalized, "d.version=?") || len(args) != 4 {
		return ""
	}
	id, _ := args[2].Value.(string)
	return id
}

func (fault *deduplicateDiscardFault) before(_ context.Context, query string, args []driver.NamedValue) error {
	fault.mutex.Lock()
	defer fault.mutex.Unlock()
	if discardStatementItem(query, args) == fault.lastID {
		fault.rejectedLast++
		return errDeduplicateDiscard
	}
	return nil
}

func (fault *deduplicateDiscardFault) after(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
	if discardStatementItem(query, args) != fault.firstID {
		return result, nil
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	fault.mutex.Lock()
	defer fault.mutex.Unlock()
	if affected == 1 {
		fault.successfulFirst++
	}
	return result, nil
}

func (fault *deduplicateDiscardFault) assertReached(t *testing.T) {
	t.Helper()
	fault.mutex.Lock()
	defer fault.mutex.Unlock()
	if fault.successfulFirst != 1 || fault.rejectedLast != 1 {
		t.Fatalf("first actual update=%d second fault=%d", fault.successfulFirst, fault.rejectedLast)
	}
}

type deduplicatePageSnapshot struct {
	Pending, Discarded, Version, UpdatedAt int64
	State, PayloadState                    string
	PayloadJob                             *string
	Items                                  []deduplicateItemSnapshot
	Jobs, Inputs                           int64
}
type deduplicateItemSnapshot struct {
	ID, State, PayloadState string
	Version, UpdatedAt      int64
	CompletedAt             *int64
	PayloadJob              *string
}

func captureDeduplicatePage(t *testing.T, fixture deduplicateFixture, importID string) deduplicatePageSnapshot {
	t.Helper()
	var result deduplicatePageSnapshot
	if err := fixture.database.QueryRowContext(fixture.ctx, `
SELECT review_pending_item_count,discarded_item_count,version,updated_at_ms,state,payload_state,payload_release_job_id
FROM import_jobs WHERE id=?`, importID).Scan(&result.Pending, &result.Discarded, &result.Version, &result.UpdatedAt,
		&result.State, &result.PayloadState, &result.PayloadJob); err != nil {
		t.Fatal(err)
	}
	rows, err := fixture.database.QueryContext(fixture.ctx, `
SELECT id,state,payload_state,version,updated_at_ms,completed_at_ms,payload_release_job_id
FROM import_items WHERE import_job_id=? ORDER BY id`, importID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	for rows.Next() {
		var item deduplicateItemSnapshot
		if err := rows.Scan(&item.ID, &item.State, &item.PayloadState, &item.Version, &item.UpdatedAt, &item.CompletedAt, &item.PayloadJob); err != nil {
			t.Fatal(err)
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fixture.database.QueryRowContext(fixture.ctx, `
SELECT count(*),(SELECT count(*) FROM job_input_snapshots input JOIN jobs child ON child.id=input.job_id
 WHERE child.scope_id=? OR child.scope_id IN (SELECT id FROM import_items WHERE import_job_id=?))
FROM jobs WHERE scope_id=? OR scope_id IN (SELECT id FROM import_items WHERE import_job_id=?)`, importID, importID, importID, importID).
		Scan(&result.Jobs, &result.Inputs); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertDeduplicatePageUnchanged(t *testing.T, fixture deduplicateFixture, importID string, before deduplicatePageSnapshot) {
	t.Helper()
	after := captureDeduplicatePage(t, fixture, importID)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("page changed across rollback: before=%+v after=%+v", before, after)
	}
}

func assertDeduplicateRetry(t *testing.T, fixture deduplicateFixture, copies ServerImportResult) {
	t.Helper()
	var count int

	for _, item := range copies.Items {
		assertDeduplicateItemState(t, fixture, item.ItemID, "DISCARDED")
	}
	after := captureDeduplicatePage(t, fixture, copies.Created.ImportJobID)
	if after.Pending != 0 || after.Discarded != 2 {
		t.Fatalf("retry aggregate = %+v", after)
	}
	if err := fixture.database.QueryRowContext(fixture.ctx, "SELECT count(*) FROM review_events WHERE event_type='DISCARDED'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("retry discard events=%d", count)
	}
	if err := fixture.database.QueryRowContext(fixture.ctx, "SELECT count(*) FROM games WHERE status='PUBLISHED'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("published games=%d", count)
	}
}
