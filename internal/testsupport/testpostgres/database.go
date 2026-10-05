// Package testpostgres provisions an isolated PostgreSQL database for each test.
package testpostgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/google/uuid"
	// Register the PostgreSQL driver used to create and drop isolated test databases.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// DSN creates an empty database and registers its removal after test cleanup.
// The supplied server must be dedicated to tests and permit CREATE DATABASE.
func DSN(t testing.TB) string {
	t.Helper()
	base := os.Getenv("RETROM_TEST_DATABASE_URL")
	if base == "" {
		t.Fatal("RETROM_TEST_DATABASE_URL is required; use make test with a dedicated PostgreSQL test server")
	}
	location, err := url.Parse(base)
	if err != nil || (location.Scheme != "postgres" && location.Scheme != "postgresql") {
		t.Fatal("RETROM_TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	name := "retrom_test_" + uuid.NewString()
	if _, err := admin.ExecContext(t.Context(), `CREATE DATABASE "`+name+`" TEMPLATE template0`); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, `DROP DATABASE "`+name+`" WITH (FORCE)`); err != nil {
			t.Error(fmt.Errorf("remove test database: %w", err))
		}
		if err := admin.Close(); err != nil {
			t.Error(err)
		}
	})
	location.Path = "/" + name
	query := location.Query()
	query.Set("default_query_exec_mode", "simple_protocol")
	location.RawQuery = query.Encode()
	return location.String()
}

// ForDatabase reconnects to a test database without creating or replacing it.
func ForDatabase(t testing.TB, name string) string {
	t.Helper()
	location, err := url.Parse(os.Getenv("RETROM_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	location.Path = "/" + name
	query := location.Query()
	query.Set("default_query_exec_mode", "simple_protocol")
	location.RawQuery = query.Encode()
	return location.String()
}

// HasCode checks a server error without depending on localized message text.
func HasCode(err error, code string) bool {
	var server *pgconn.PgError
	return errors.As(err, &server) && server.Code == code
}
