//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"retrom/internal/testsupport"
)

func TestApprovePreservesEvidenceReadFailure(t *testing.T) {
	t.Parallel()
	fixture := newDeduplicateFixture(t)
	created := fixture.create(t, "Approve read failure", "Retrom owned approval regression", 1)
	before := captureDeduplicatePage(t, fixture, created.Created.ImportJobID)
	cause := errors.New("approval evidence unavailable")
	hits := 0
	fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.Contains(query, "FROM import_items i") && strings.Contains(query, "JOIN import_items d") {
				hits++
				return cause
			}
			return nil
		},
	})
	result, err := fixture.service.Approve(t.Context(), created.Items[0].ItemID, 1)
	if !errors.Is(err, cause) || errors.Is(err, ErrInvalid) || result != (Approved{}) || hits != 1 {
		t.Fatalf("approval evidence failure lost cause: result=%+v err=%v hits=%d", result, err, hits)
	}
	assertDeduplicatePageUnchanged(t, fixture, created.Created.ImportJobID, before)
}

func TestApproveMalformedMetadataPreservesJSONFailure(t *testing.T) {
	t.Parallel()
	fixture := newDeduplicateFixture(t)
	created := fixture.create(t, "Approve invalid evidence", "Retrom owned approval JSON regression", 1)
	itemID := created.Items[0].ItemID
	fixture.execute(t, `UPDATE import_items SET metadata_json='{' WHERE id=?`, itemID)
	before := captureDeduplicatePage(t, fixture, created.Created.ImportJobID)
	result, err := fixture.service.Approve(t.Context(), itemID, 1)
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) || result != (Approved{}) {
		t.Fatalf("invalid approval JSON cause lost: result=%+v err=%v", result, err)
	}
	assertDeduplicatePageUnchanged(t, fixture, created.Created.ImportJobID, before)
}
