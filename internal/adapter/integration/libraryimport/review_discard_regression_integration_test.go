//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"retrom/internal/testkit/testsupport"
)

func TestDiscardPreservesEvidenceReadFailure(t *testing.T) {
	t.Parallel()
	fixture := newDeduplicateFixture(t)
	created := fixture.create(t, "Discard read failure", "Retrom owned discard regression", 1)
	itemID := created.Items[0].ItemID
	cause := errors.New("discard evidence database unavailable")
	hits := 0
	fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.Contains(query, "FROM import_items i") && strings.Contains(query, "JOIN review_drafts d") {
				hits++
				return cause
			}
			return nil
		},
	})
	result, err := fixture.service.Discard(t.Context(), itemID, 1, "")
	if !errors.Is(err, cause) || errors.Is(err, ErrInvalid) || result != (DecisionResult{}) || hits != 1 {
		t.Fatalf("discard evidence failure lost cause: result=%+v err=%v hits=%d", result, err, hits)
	}
	assertDeduplicateItemState(t, fixture, itemID, "REVIEW_PENDING")
}

func TestDiscardRejectsPegasusReviewBeforeHandoff(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	created, err := fixture.service.CreateOwnedServerSource(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	itemID := created.Items[0].ItemID
	before := captureDeduplicatePage(t, fixture, created.Created.ImportJobID)
	var beforeState string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT execution_state FROM pegasus_import_items WHERE id=?`, request.Intent.ItemID).Scan(&beforeState); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.Discard(t.Context(), itemID, 1, "")
	if !errors.Is(err, ErrInvalid) || result != (DecisionResult{}) {
		t.Fatalf("unhanded Pegasus review lacked domain rejection: result=%+v err=%v", result, err)
	}
	assertDeduplicatePageUnchanged(t, fixture, created.Created.ImportJobID, before)
	var state string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT execution_state FROM pegasus_import_items WHERE id=?`, request.Intent.ItemID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != beforeState {
		t.Fatalf("unhanded owner changed to %s", state)
	}
}

func TestDiscardMalformedMetadataPreservesJSONFailure(t *testing.T) {
	t.Parallel()
	fixture := newDeduplicateFixture(t)
	created := fixture.create(t, "Discard invalid evidence", "Retrom owned malformed evidence fixture", 1)
	itemID := created.Items[0].ItemID
	fixture.execute(t, `UPDATE review_drafts SET metadata_json='{' WHERE import_item_id=?`, itemID)
	before := captureDeduplicatePage(t, fixture, created.Created.ImportJobID)
	result, err := fixture.service.Discard(t.Context(), itemID, 1, "")
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) || result != (DecisionResult{}) {
		t.Fatalf("invalid metadata JSON cause lost: result=%+v err=%v", result, err)
	}
	assertDeduplicatePageUnchanged(t, fixture, created.Created.ImportJobID, before)
}
