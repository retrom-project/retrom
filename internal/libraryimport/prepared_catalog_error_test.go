//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"retrom/internal/testsupport"
)

func TestPreparedCreationPreservesActiveDATReadFailure(t *testing.T) {
	fixture, request := ownedSourceFixture(t)
	cause := errors.New("active DAT snapshot unavailable")
	reads := 0
	fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.Contains(query, "FROM dat_versions WHERE provider_id=? AND target_id=? AND is_active=1") {
				reads++
				return cause
			}
			return nil
		},
	})
	result, err := fixture.service.CreateServerSource(fixture.ctx, fixture.platform, "STANDARD", request.Files, nil, "")
	if !errors.Is(err, cause) || result.Created.ImportJobID != "" || result.Items != nil || reads != 1 {
		t.Fatalf("DAT failure became successful import: result=%+v reads=%d err=%v", result, reads, err)
	}
	var items int
	if err := fixture.database.QueryRowContext(fixture.ctx, `SELECT count(*) FROM import_items`).Scan(&items); err != nil {
		t.Fatal(err)
	}
	if items != 0 {
		t.Fatalf("failed DAT snapshot committed %d items", items)
	}
}
