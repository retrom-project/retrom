package favorites

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"testing"

	"retrom/internal/testsupport"
)

func favoriteMembershipFault(t *testing.T, db *sql.DB, cause error) (*sql.DB, func()) {
	t.Helper()
	first, hits := 0, 0
	match := func(query string, args []driver.NamedValue, gameID string) bool {
		return strings.HasPrefix(strings.TrimSpace(query), "INSERT INTO favorite_folder_games") && len(args) == 4 && args[2].Value == gameID
	}
	fault := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if match(query, args, testGameB) {
				hits++
				return cause
			}
			return nil
		},
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if match(query, args, testGameA) {
				first++
			}
			return result, nil
		},
	})
	return fault, func() {
		t.Helper()
		if first != 1 || hits != 1 {
			t.Fatalf("membership fault did not follow actual first insert: first=%d hits=%d", first, hits)
		}
	}
}
