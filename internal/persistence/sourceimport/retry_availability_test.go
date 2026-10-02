package sourceimport

import (
	"errors"
	"testing"
	"time"

	application "retrom/internal/service/sourceimport"
)

func TestRetryProjectionAndCommandUseCurrentItems(t *testing.T) {
	db := workflowDatabase(t)
	if _, err := db.ExecContext(t.Context(), "UPDATE source_import_items SET retryable=0 WHERE import_id='import-0'"); err != nil {
		t.Fatal(err)
	}
	summary, err := NewQueries(db).Get(t.Context(), "import-0")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Retryable {
		t.Fatal("stale plan retryable flag escaped current item facts")
	}
	if _, err := db.ExecContext(t.Context(), "UPDATE source_import_items SET execution_state='READ_FAILED',retryable=1 WHERE id='item-0'; UPDATE source_imports SET retryable=0 WHERE id='import-0'"); err != nil {
		t.Fatal(err)
	}
	summary, err = NewQueries(db).Get(t.Context(), "import-0")
	if err != nil {
		t.Fatal(err)
	}
	if !summary.Retryable {
		t.Fatal("eligible item hidden by stale plan flag")
	}
	values, err := NewQueries(db).List(t.Context(), application.ListQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || !values[0].Retryable {
		t.Fatalf("list disagrees with detail: %#v", values)
	}
	_, err = application.NewWorkflowControl(NewWorkflowControl(db), func() time.Time { return time.UnixMilli(10) }).Retry(t.Context(), "import-0", summary.Version, "actor")
	if err != nil {
		t.Fatal(err)
	}
}

func TestChangedSourceCannotRetryEvenWithStaleItemFlag(t *testing.T) {
	db := workflowDatabase(t)
	if _, err := db.ExecContext(t.Context(), "UPDATE source_import_items SET execution_state='SOURCE_CHANGED',retryable=1 WHERE id='item-0'"); err != nil {
		t.Fatal(err)
	}
	summary, err := NewQueries(db).Get(t.Context(), "import-0")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Retryable {
		t.Fatal("changed source reused a stale scan snapshot")
	}
	_, err = application.NewWorkflowControl(NewWorkflowControl(db), time.Now).Retry(t.Context(), "import-0", summary.Version, "actor")
	if !errors.Is(err, application.ErrNotRetryable) {
		t.Fatalf("retry error = %v", err)
	}
}
